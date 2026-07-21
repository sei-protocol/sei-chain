package operations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	commonevm "github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func StateSizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state-size",
		Short: "Print analytical results for state size",
		Run:   executeStateSize,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "memIAVL database directory")
	cmd.PersistentFlags().Int64("height", 0, "Block Height")
	cmd.PersistentFlags().StringP("module", "m", "", "Module to export. Default to export all")

	// FlatKV integration: optional FlatKV data directory. When non-empty (or
	// when a sibling flatkv/ dir is auto-detected next to --db-dir) the tool
	// also scans FlatKV and folds the result into the same console output
	// and the same DynamoDB batch as the memIAVL module rows.
	cmd.PersistentFlags().String("flatkv-dir", "", "FlatKV data directory (default: auto-detect <db-dir>/../flatkv)")

	// DynamoDB export flags
	cmd.PersistentFlags().Bool("export-dynamodb", false, "Export results to DynamoDB instead of printing")
	cmd.PersistentFlags().String("dynamodb-table", "state_size_analysis", "DynamoDB table name")
	cmd.PersistentFlags().String("aws-region", "us-east-2", "AWS region for DynamoDB")

	return cmd
}

func executeStateSize(cmd *cobra.Command, _ []string) {
	module, _ := cmd.Flags().GetString("module")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	flatkvDir, _ := cmd.Flags().GetString("flatkv-dir")
	height, _ := cmd.Flags().GetInt64("height")
	exportDynamoDB, _ := cmd.Flags().GetBool("export-dynamodb")
	dynamoDBTable, _ := cmd.Flags().GetString("dynamodb-table")
	awsRegion, _ := cmd.Flags().GetString("aws-region")

	if dbDir == "" {
		panic("Must provide database dir")
	}

	opts := memiavl.Options{
		Dir:             dbDir,
		ZeroCopy:        true,
		CreateIfMissing: false,
	}
	db, err := memiavl.OpenDB(height, opts)
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()

	actualHeight := db.Version()
	fmt.Printf("Finished opening db at height %d (requested: %d), calculating state size for module: %s\n", actualHeight, height, module)

	moduleResults := collectAllModuleData(module, db)

	// Optionally scan FlatKV at the same requested height. We only bother
	// when --module is empty or "evm" because FlatKV in production holds
	// only evm keys (anything else is bucketed into the "misc" DB).
	flatkvResult, flatkvActualHeight := maybeCollectFlatKV(flatkvDir, dbDir, module, height)

	if exportDynamoDB {
		fmt.Printf("Exporting results to DynamoDB table: %s\n", dynamoDBTable)
		var extras []*utils.StateSizeAnalysis
		if flatkvResult != nil {
			extras = append(extras, flatkvStateSizeAnalysis(flatkvResult, flatkvActualHeight))
		}
		if err := exportResultsToDynamoDB(moduleResults, extras, actualHeight, dynamoDBTable, awsRegion); err != nil {
			panic(err)
		}
		fmt.Println("Successfully exported to DynamoDB!")
	} else {
		printResultsToConsole(moduleResults)
		if flatkvResult != nil {
			printFlatKVResults(flatkvResult, flatkvActualHeight)
		}
	}
}

// resolveFlatKVDir returns the FlatKV directory to scan, if any.
//
//   - if --flatkv-dir was supplied explicitly, it is returned as-is (the
//     caller is responsible for the path being valid).
//   - otherwise the tool auto-detects a sibling "flatkv/" directory next
//     to --db-dir (e.g. <home>/data/committer.db -> <home>/data/flatkv),
//     which is the standard layout on a seid shadow node. Returns "" if
//     no such sibling exists.
func resolveFlatKVDir(flatkvDir, dbDir string) string {
	if flatkvDir != "" {
		return flatkvDir
	}
	candidate := filepath.Join(filepath.Dir(dbDir), "flatkv")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	return ""
}

// maybeCollectFlatKV resolves the FlatKV directory and, if present and the
// caller's --module filter is compatible, opens a read-only clone of the
// FlatKV store and scans it.
//
// On any error (dir missing, snapshot unavailable, open failure) we log the
// reason and return a nil result so the memIAVL path still succeeds. FlatKV
// analysis is strictly additive; failing here must never take down the
// existing state-size workflow.
func maybeCollectFlatKV(flatkvDir, dbDir, module string, height int64) (*FlatKVStateSizeResult, int64) {
	if module != "" && module != commonevm.EVMStoreKey {
		return nil, 0
	}
	dir := resolveFlatKVDir(flatkvDir, dbDir)
	if dir == "" {
		return nil, 0
	}

	fmt.Printf("\nAnalyzing FlatKV at %s (requested height: %d)\n", dir, height)
	store, err := openFlatKVReadOnly(dir, height)
	if err != nil {
		fmt.Printf("FlatKV analysis skipped: %v\n", err)
		return nil, 0
	}
	defer func() { _ = store.Close() }()

	actualHeight := store.Version()
	fmt.Printf("Opened FlatKV at version %d\n", actualHeight)
	result, err := collectFlatKVStateSize(store.CommitStore)
	if err != nil {
		fmt.Printf("FlatKV analysis skipped: %v\n", err)
		return nil, 0
	}
	return result, actualHeight
}

// collectModuleStats collects all the statistics for a module
func collectModuleStats(tree *memiavl.Tree, moduleName string) *ModuleResult {
	result := &ModuleResult{
		ModuleName:    moduleName,
		PrefixSizes:   make(map[string]*utils.PrefixSize),
		ContractSizes: make(map[string]*utils.ContractSizeEntry),
	}

	// Scan the tree to collect statistics
	tree.ScanPostOrder(func(node memiavl.Node) bool {
		if node.IsLeaf() {
			result.TotalNumKeys++
			keySize := len(node.Key())
			valueSize := len(node.Value())
			totalSize := uint64(keySize + valueSize) //nolint:gosec

			result.TotalKeySize += uint64(keySize)
			result.TotalValueSize += uint64(valueSize)
			result.TotalSize += totalSize

			prefixKey := fmt.Sprintf("%X", node.Key())
			prefix := prefixKey[:2]
			if _, exists := result.PrefixSizes[prefix]; !exists {
				result.PrefixSizes[prefix] = &utils.PrefixSize{}
			}
			result.PrefixSizes[prefix].KeySize += uint64(keySize)
			result.PrefixSizes[prefix].ValueSize += uint64(valueSize)
			result.PrefixSizes[prefix].TotalSize += totalSize
			result.PrefixSizes[prefix].KeyCount++

			// Handle EVM contract analysis
			if moduleName == commonevm.EVMStoreKey && prefix == "03" {
				addr := prefixKey[2:42]
				if _, exists := result.ContractSizes[addr]; !exists {
					result.ContractSizes[addr] = &utils.ContractSizeEntry{Address: addr}
				}
				entry := result.ContractSizes[addr]
				entry.TotalSize += totalSize
				entry.KeyCount++
			}

			// Progress every 10M keys. The largest module (evm) holds
			// hundreds of millions of leaves; a 1M-per-line cadence here
			// was drowning the actual report in 700+ progress lines.
			if result.TotalNumKeys%10000000 == 0 {
				fmt.Printf("Scanned %d keys for module %s\n", result.TotalNumKeys, moduleName)
			}
		}
		return true
	})

	// Limit to top 100 contracts by total size
	result.ContractSizes = limitToTopContracts(result.ContractSizes, 100)

	return result
}

// limitToTopContracts keeps only the top N contracts by total size
func limitToTopContracts(contracts map[string]*utils.ContractSizeEntry, limit int) map[string]*utils.ContractSizeEntry {
	if len(contracts) <= limit {
		return contracts
	}

	// Convert to slice for sorting
	contractSlice := make([]utils.ContractSizeEntry, 0, len(contracts))
	for _, contract := range contracts {
		contractSlice = append(contractSlice, *contract)
	}

	// Sort by total size in descending order
	sort.Slice(contractSlice, func(i, j int) bool {
		return contractSlice[i].TotalSize > contractSlice[j].TotalSize
	})

	// Keep only top N
	result := make(map[string]*utils.ContractSizeEntry)
	for i := 0; i < limit; i++ {
		contract := contractSlice[i]
		result[contract.Address] = &contract
	}

	return result
}

// ModuleResult holds the complete analysis results for a single module
type ModuleResult struct {
	ModuleName     string
	TotalNumKeys   uint64
	TotalKeySize   uint64
	TotalValueSize uint64
	TotalSize      uint64
	PrefixSizes    map[string]*utils.PrefixSize
	ContractSizes  map[string]*utils.ContractSizeEntry
}

// collectAllModuleData scans all modules and collects statistics in memory
func collectAllModuleData(module string, db *memiavl.DB) map[string]*ModuleResult {
	modules := []string{}
	if module == "" {
		modules = AllModules
	} else {
		modules = append(modules, module)
	}

	moduleResults := make(map[string]*ModuleResult)

	for _, moduleName := range modules {
		tree := db.TreeByName(moduleName)
		if tree == nil {
			fmt.Printf("Tree does not exist for module %s, skipping...\n", moduleName)
			continue
		}

		fmt.Printf("Analyzing module: %s\n", moduleName)

		// Collect statistics directly into ModuleResult
		result := collectModuleStats(tree, moduleName)

		// Store in memory (result is already a ModuleResult)
		moduleResults[moduleName] = result

		fmt.Printf("Collected stats for module %s: %d keys, %d total size\n",
			moduleName, result.TotalNumKeys, result.TotalSize)
	}

	return moduleResults
}

// exportResultsToDynamoDB exports the collected memIAVL module results plus
// any additional pre-built analyses (e.g. FlatKV) to DynamoDB as a single
// batch. The metadata latest-height record is keyed off the memIAVL height
// because it remains the canonical "observation height" even when FlatKV
// resolved to a slightly older snapshot.
func exportResultsToDynamoDB(
	moduleResults map[string]*ModuleResult,
	extras []*utils.StateSizeAnalysis,
	height int64,
	tableName, awsRegion string,
) error {
	dynamoClient, err := utils.NewDynamoDBClient(tableName, awsRegion)
	if err != nil {
		return fmt.Errorf("failed to create DynamoDB client: %w", err)
	}

	analyses := make([]*utils.StateSizeAnalysis, 0, len(moduleResults)+len(extras))
	for _, result := range moduleResults {
		analyses = append(analyses, createStateSizeAnalysis(height, result.ModuleName, result))
	}
	analyses = append(analyses, extras...)

	if err := dynamoClient.ExportMultipleAnalyses(analyses); err != nil {
		return fmt.Errorf("failed to export analyses to DynamoDB: %w", err)
	}

	metadataTableName := tableName + "_metadata"
	_, err = dynamoClient.UpdateLatestHeightIfGreater(metadataTableName, height)
	return err
}

// printResultsToConsole prints the collected results to console.
//
// PrefixSizes is keyed by hex prefix byte (e.g. "03", "0A"), not by module
// name, so previous code that indexed this map with the module name always
// panicked on nil deref the first time the console path was taken. We now
// marshal the entire map per module, which is what "prefix breakdown" was
// always meant to surface.
//
// Modules are emitted in alphabetical order so successive runs produce
// diffable output. The top-contracts table is skipped for modules without
// any 0x03 entries to avoid printing empty table headers for every non-evm
// module.
func printResultsToConsole(moduleResults map[string]*ModuleResult) {
	names := make([]string, 0, len(moduleResults))
	for name := range moduleResults {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		result := moduleResults[name]
		fmt.Printf("Module %s total numKeys:%d, total keySize:%d, total valueSize:%d, totalSize:%d (%.2f MB)\n",
			result.ModuleName, result.TotalNumKeys, result.TotalKeySize, result.TotalValueSize,
			result.TotalSize, float64(result.TotalSize)/1024/1024)

		prefixJSON, err := json.MarshalIndent(result.PrefixSizes, "", "  ")
		if err != nil {
			fmt.Printf("Module %s failed to marshal prefix breakdown: %v\n", result.ModuleName, err)
		} else {
			fmt.Printf("Module %s prefix breakdown (key/value/total bytes and key count per prefix byte): %s\n",
				result.ModuleName, prefixJSON)
		}

		if len(result.ContractSizes) == 0 {
			continue
		}

		fmt.Printf("\nDetailed breakdown for 0x03 prefix (top %d contracts by total size):\n", len(result.ContractSizes))
		fmt.Printf("%-42s %15s %10s\n", "Contract Address", "Total Size", "Key Count")
		fmt.Printf("%s\n", strings.Repeat("-", 70))

		contractSlice := make([]utils.ContractSizeEntry, 0, len(result.ContractSizes))
		for _, entry := range result.ContractSizes {
			contractSlice = append(contractSlice, *entry)
		}
		sort.Slice(contractSlice, func(i, j int) bool {
			return contractSlice[i].TotalSize > contractSlice[j].TotalSize
		})

		for _, contract := range contractSlice {
			fmt.Printf("0x%-40s %15d %10d\n",
				contract.Address,
				contract.TotalSize,
				contract.KeyCount)
		}
	}
}

// createStateSizeAnalysis creates a new StateSizeAnalysis from ModuleResult
func createStateSizeAnalysis(blockHeight int64, moduleName string, result *ModuleResult) *utils.StateSizeAnalysis {
	// Convert raw data to JSON strings for DynamoDB storage

	prefixJSON, _ := json.Marshal(result.PrefixSizes)

	contractSlice := make([]utils.ContractSizeEntry, 0, len(result.ContractSizes))
	for _, contract := range result.ContractSizes {
		contractSlice = append(contractSlice, *contract)
	}
	contractJSON, _ := json.Marshal(contractSlice)

	return &utils.StateSizeAnalysis{
		BlockHeight:       blockHeight,
		ModuleName:        moduleName,
		TotalNumKeys:      result.TotalNumKeys,
		TotalKeySize:      result.TotalKeySize,
		TotalValueSize:    result.TotalValueSize,
		TotalSize:         result.TotalSize,
		PrefixBreakdown:   string(prefixJSON),
		ContractBreakdown: string(contractJSON),
	}
}
