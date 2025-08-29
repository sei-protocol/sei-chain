package operations

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	"github.com/sei-protocol/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func StateSizeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state-size",
		Short: "Print analytical results for state size",
		Run:   executeStateSize,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	cmd.PersistentFlags().Int64("height", 0, "Block Height")
	cmd.PersistentFlags().StringP("module", "m", "", "Module to export. Default to export all")

	// DynamoDB export flags
	cmd.PersistentFlags().Bool("export-dynamodb", false, "Export results to DynamoDB instead of printing")
	cmd.PersistentFlags().String("dynamodb-table", "state-size-analysis", "DynamoDB table name")
	cmd.PersistentFlags().String("aws-region", "us-east-2", "AWS region for DynamoDB")

	return cmd
}

func executeStateSize(cmd *cobra.Command, _ []string) {
	module, _ := cmd.Flags().GetString("module")
	dbDir, _ := cmd.Flags().GetString("db-dir")
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
	db, err := memiavl.OpenDB(logger.NewNopLogger(), height, opts)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Get the actual height of the opened database
	actualHeight := db.Version()
	fmt.Printf("Finished opening db at height %d (requested: %d), calculating state size for module: %s\n", actualHeight, height, module)

	// First, collect all the data by scanning the trees
	moduleResults := collectAllModuleData(module, db)

	// Then process the results based on the flag
	if exportDynamoDB {
		fmt.Printf("Exporting results to DynamoDB table: %s\n", dynamoDBTable)
		err = exportResultsToDynamoDB(moduleResults, actualHeight, dynamoDBTable, awsRegion)
		if err != nil {
			panic(err)
		}
		fmt.Println("Successfully exported to DynamoDB!")
	} else {
		printResultsToConsole(moduleResults)
	}
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
			result.TotalKeySize += uint64(keySize)
			result.TotalValueSize += uint64(valueSize)
			result.TotalSize += uint64(keySize + valueSize)

			prefixKey := fmt.Sprintf("%X", node.Key())
			prefix := prefixKey[:2]
			if _, exists := result.PrefixSizes[prefix]; !exists {
				result.PrefixSizes[prefix] = &utils.PrefixSize{}
			}
			result.PrefixSizes[prefix].KeySize += uint64(keySize)
			result.PrefixSizes[prefix].ValueSize += uint64(valueSize)
			result.PrefixSizes[prefix].TotalSize += uint64(keySize + valueSize)
			result.PrefixSizes[prefix].KeyCount++

			// Handle EVM contract analysis
			if moduleName == "evm" && prefix == "03" {
				addr := prefixKey[2:42]
				if _, exists := result.ContractSizes[addr]; !exists {
					result.ContractSizes[addr] = &utils.ContractSizeEntry{Address: addr}
				}
				entry := result.ContractSizes[addr]
				entry.TotalSize += uint64(len(node.Key()) + len(node.Value()))
				entry.KeyCount++
			}

			if result.TotalNumKeys%1000000 == 0 {
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
	var contractSlice []utils.ContractSizeEntry
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

// exportResultsToDynamoDB exports the collected results to DynamoDB
func exportResultsToDynamoDB(moduleResults map[string]*ModuleResult, height int64, tableName, awsRegion string) error {
	// Initialize DynamoDB client
	dynamoClient, err := utils.NewDynamoDBClient(tableName, awsRegion)
	if err != nil {
		return fmt.Errorf("failed to create DynamoDB client: %w", err)
	}

	var analyses []*utils.StateSizeAnalysis

	for _, result := range moduleResults {
		// Create analysis object directly from raw data
		analysis := createStateSizeAnalysis(height, result.ModuleName, result)
		analyses = append(analyses, analysis)
	}

	// Export all analyses to DynamoDB
	if err := dynamoClient.ExportMultipleAnalyses(analyses); err != nil {
		return fmt.Errorf("failed to export analyses to DynamoDB: %w", err)
	}

	return nil
}

// printResultsToConsole prints the collected results to console
func printResultsToConsole(moduleResults map[string]*ModuleResult) {

	for moduleName, result := range moduleResults {
		fmt.Printf("Module %s total numKeys:%d, total keySize:%d, total valueSize:%d, totalSize: %d \n",
			result.ModuleName, result.TotalNumKeys, result.TotalKeySize, result.TotalValueSize, result.TotalSize)

		prefixKeyResult, _ := json.MarshalIndent(result.PrefixSizes[moduleName].KeySize, "", "  ")
		fmt.Printf("Module %s prefix key size breakdown (bytes): %s \n", result.ModuleName, prefixKeyResult)

		prefixValueResult, _ := json.MarshalIndent(result.PrefixSizes[moduleName].ValueSize, "", "  ")
		fmt.Printf("Module %s prefix value size breakdown (bytes): %s \n", result.ModuleName, prefixValueResult)

		totalSizeResult, _ := json.MarshalIndent(result.PrefixSizes[moduleName].TotalSize, "", "  ")
		fmt.Printf("Module %s prefix total size breakdown (bytes): %s \n", result.ModuleName, totalSizeResult)

		numKeysResult, _ := json.MarshalIndent(result.PrefixSizes[moduleName].KeyCount, "", "  ")
		fmt.Printf("Module %s prefix num of keys breakdown: %s \n", result.ModuleName, numKeysResult)

		// Display top contracts (already limited to top 100)
		fmt.Printf("\nDetailed breakdown for 0x03 prefix (top %d contracts by total size):\n", len(result.ContractSizes))
		fmt.Printf("%-42s %15s %10s\n", "Contract Address", "Total Size", "Key Count")
		fmt.Printf("%s\n", strings.Repeat("-", 70))

		// Convert to slice for display
		var contractSlice []utils.ContractSizeEntry
		for _, entry := range result.ContractSizes {
			contractSlice = append(contractSlice, *entry)
		}

		// Sort by total size in descending order for display
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

	var contractSlice []utils.ContractSizeEntry
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
