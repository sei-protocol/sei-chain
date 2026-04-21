package operations

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func FlatKVStateSizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flatkv-state-size",
		Short: "Analyze FlatKV state size: key/value size breakdown per DB, prefix, and EVM contract",
		Run:   executeFlatKVStateSize,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "FlatKV data directory")
	cmd.PersistentFlags().Int64("height", 0, "Block height (0 = latest)")

	cmd.PersistentFlags().Bool("export-dynamodb", false, "Export results to DynamoDB")
	cmd.PersistentFlags().String("dynamodb-table", "flatkv_state_size_analysis", "DynamoDB table name")
	cmd.PersistentFlags().String("aws-region", "us-east-2", "AWS region for DynamoDB")

	return cmd
}

func executeFlatKVStateSize(cmd *cobra.Command, _ []string) {
	dbDir, _ := cmd.Flags().GetString("db-dir")
	height, _ := cmd.Flags().GetInt64("height")
	exportDynamoDB, _ := cmd.Flags().GetBool("export-dynamodb")
	dynamoDBTable, _ := cmd.Flags().GetString("dynamodb-table")
	awsRegion, _ := cmd.Flags().GetString("aws-region")

	if dbDir == "" {
		panic("Must provide database dir")
	}

	store, err := openFlatKVReadOnly(dbDir, height)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	actualHeight := store.Version()
	fmt.Printf("Opened FlatKV at version %d (requested: %d)\n", actualHeight, height)

	result := collectFlatKVStateSize(store.CommitStore)

	if exportDynamoDB {
		fmt.Printf("Exporting results to DynamoDB table: %s\n", dynamoDBTable)
		if err := exportFlatKVResultsToDynamoDB(result, actualHeight, dynamoDBTable, awsRegion); err != nil {
			panic(err)
		}
		fmt.Println("Successfully exported to DynamoDB!")
	} else {
		printFlatKVResults(result)
	}
}

// FlatKVStateSizeResult holds the complete analysis of a FlatKV store.
type FlatKVStateSizeResult struct {
	TotalNumKeys   uint64
	TotalKeySize   uint64
	TotalValueSize uint64
	TotalSize      uint64

	// Per-DB breakdown (account, code, storage, legacy)
	DBSizes map[string]*FlatKVDBSize

	// Top EVM contracts by storage size
	ContractSizes map[string]*utils.ContractSizeEntry
}

// FlatKVDBSize holds size stats for one logical DB.
type FlatKVDBSize struct {
	NumKeys   uint64
	KeySize   uint64
	ValueSize uint64
	TotalSize uint64
}

func collectFlatKVStateSize(store *flatkv.CommitStore) *FlatKVStateSizeResult {
	result := &FlatKVStateSizeResult{
		DBSizes:       make(map[string]*FlatKVDBSize),
		ContractSizes: make(map[string]*utils.ContractSizeEntry),
	}

	iter := store.RawGlobalIterator()
	defer func() { _ = iter.Close() }()

	if !iter.First() {
		return result
	}

	for iter.Valid() {
		key := iter.Key()
		value := iter.Value()
		keySize := uint64(len(key))
		valueSize := uint64(len(value))
		totalSize := keySize + valueSize

		result.TotalNumKeys++
		result.TotalKeySize += keySize
		result.TotalValueSize += valueSize
		result.TotalSize += totalSize

		dbName := classifyPhysicalKey(key)
		if _, ok := result.DBSizes[dbName]; !ok {
			result.DBSizes[dbName] = &FlatKVDBSize{}
		}
		db := result.DBSizes[dbName]
		db.NumKeys++
		db.KeySize += keySize
		db.ValueSize += valueSize
		db.TotalSize += totalSize

		// Track EVM contract storage sizes (storage DB, 0x03 prefix)
		if dbName == "storage" {
			addr := extractContractAddress(key)
			if addr != "" {
				if _, ok := result.ContractSizes[addr]; !ok {
					result.ContractSizes[addr] = &utils.ContractSizeEntry{Address: addr}
				}
				entry := result.ContractSizes[addr]
				entry.TotalSize += totalSize
				entry.KeyCount++
			}
		}

		if result.TotalNumKeys%1000000 == 0 {
			fmt.Printf("  scanned %d keys...\n", result.TotalNumKeys)
		}

		iter.Next()
	}

	result.ContractSizes = limitFlatKVTopContracts(result.ContractSizes, 100)
	return result
}

// classifyPhysicalKey determines which logical DB a physical key belongs to.
// Physical format: "module/" + type_prefix_byte + stripped_key.
func classifyPhysicalKey(key []byte) string {
	moduleName, innerKey, err := ktype.StripModulePrefix(key)
	if err != nil || moduleName != "evm" {
		return "legacy"
	}
	if len(innerKey) == 0 {
		return "legacy"
	}
	switch innerKey[0] {
	case 0x0a: // nonce/codehash (account DB)
		return "account"
	case 0x07: // code
		return "code"
	case 0x03: // storage
		return "storage"
	default:
		return "legacy"
	}
}

// extractContractAddress extracts the hex address from a storage physical key.
// Physical format: "evm/" + 0x03 + addr(20) + slot(32).
func extractContractAddress(key []byte) string {
	_, innerKey, err := ktype.StripModulePrefix(key)
	if err != nil || len(innerKey) < 21 {
		return ""
	}
	// innerKey[0] is 0x03 prefix, next 20 bytes are address
	return fmt.Sprintf("%X", innerKey[1:21])
}

func limitFlatKVTopContracts(contracts map[string]*utils.ContractSizeEntry, limit int) map[string]*utils.ContractSizeEntry {
	if len(contracts) <= limit {
		return contracts
	}
	slice := make([]utils.ContractSizeEntry, 0, len(contracts))
	for _, c := range contracts {
		slice = append(slice, *c)
	}
	sort.Slice(slice, func(i, j int) bool { return slice[i].TotalSize > slice[j].TotalSize })

	result := make(map[string]*utils.ContractSizeEntry, limit)
	for i := 0; i < limit; i++ {
		c := slice[i]
		result[c.Address] = &c
	}
	return result
}

func printFlatKVResults(r *FlatKVStateSizeResult) {
	fmt.Printf("\n=== FlatKV State Size ===\n")
	fmt.Printf("Total keys:       %d\n", r.TotalNumKeys)
	fmt.Printf("Total key size:   %d bytes (%.2f MB)\n", r.TotalKeySize, float64(r.TotalKeySize)/1024/1024)
	fmt.Printf("Total value size: %d bytes (%.2f MB)\n", r.TotalValueSize, float64(r.TotalValueSize)/1024/1024)
	fmt.Printf("Total size:       %d bytes (%.2f MB)\n", r.TotalSize, float64(r.TotalSize)/1024/1024)

	fmt.Printf("\n--- Per-DB Breakdown ---\n")
	fmt.Printf("%-12s %15s %15s %15s %15s\n", "DB", "Keys", "Key Size", "Value Size", "Total Size")
	fmt.Printf("%s\n", strings.Repeat("-", 75))

	dbOrder := []string{"account", "code", "storage", "legacy"}
	for _, name := range dbOrder {
		db, ok := r.DBSizes[name]
		if !ok {
			continue
		}
		fmt.Printf("%-12s %15d %12d KB %12d KB %12d KB\n",
			name, db.NumKeys, db.KeySize/1024, db.ValueSize/1024, db.TotalSize/1024)
	}

	if len(r.ContractSizes) > 0 {
		fmt.Printf("\n--- Top EVM Contracts by Storage Size ---\n")
		fmt.Printf("%-42s %15s %10s\n", "Contract Address", "Total Size", "Key Count")
		fmt.Printf("%s\n", strings.Repeat("-", 70))

		slice := make([]utils.ContractSizeEntry, 0, len(r.ContractSizes))
		for _, c := range r.ContractSizes {
			slice = append(slice, *c)
		}
		sort.Slice(slice, func(i, j int) bool { return slice[i].TotalSize > slice[j].TotalSize })

		for _, c := range slice {
			fmt.Printf("0x%-40s %15d %10d\n", c.Address, c.TotalSize, c.KeyCount)
		}
	}
}

func exportFlatKVResultsToDynamoDB(r *FlatKVStateSizeResult, height int64, tableName, awsRegion string) error {
	client, err := utils.NewDynamoDBClient(tableName, awsRegion)
	if err != nil {
		return fmt.Errorf("failed to create DynamoDB client: %w", err)
	}

	// Build per-DB prefix breakdown as a JSON map compatible with the memiavl format
	prefixMap := make(map[string]*utils.PrefixSize)
	for dbName, db := range r.DBSizes {
		prefixMap[dbName] = &utils.PrefixSize{
			KeySize:   db.KeySize,
			ValueSize: db.ValueSize,
			TotalSize: db.TotalSize,
			KeyCount:  db.NumKeys,
		}
	}
	prefixJSON, _ := json.Marshal(prefixMap)

	contractSlice := make([]utils.ContractSizeEntry, 0, len(r.ContractSizes))
	for _, c := range r.ContractSizes {
		contractSlice = append(contractSlice, *c)
	}
	contractJSON, _ := json.Marshal(contractSlice)

	analysis := &utils.StateSizeAnalysis{
		BlockHeight:       height,
		ModuleName:        "flatkv",
		TotalNumKeys:      r.TotalNumKeys,
		TotalKeySize:      r.TotalKeySize,
		TotalValueSize:    r.TotalValueSize,
		TotalSize:         r.TotalSize,
		PrefixBreakdown:   string(prefixJSON),
		ContractBreakdown: string(contractJSON),
	}

	if err := client.ExportMultipleAnalyses([]*utils.StateSizeAnalysis{analysis}); err != nil {
		return fmt.Errorf("failed to export analysis to DynamoDB: %w", err)
	}

	metadataTable := tableName + "_metadata"
	_, err = client.UpdateLatestHeightIfGreater(metadataTable, height)
	return err
}
