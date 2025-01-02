package operations

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/sc/memiavl"
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
	return cmd
}

func executeStateSize(cmd *cobra.Command, _ []string) {
	module, _ := cmd.Flags().GetString("module")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	height, _ := cmd.Flags().GetInt64("height")
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
	err = PrintStateSize(module, db)
	if err != nil {
		panic(err)
	}
}

// PrintStateSize print the raw keys and values for given module at given height for memIAVL tree
func PrintStateSize(module string, db *memiavl.DB) error {
	modules := []string{}
	if module == "" {
		modules = AllModules
	} else {
		modules = append(modules, module)
	}

	for _, moduleName := range modules {
		tree := db.TreeByName(moduleName)
		totalNumKeys := 0
		totalKeySize := 0
		totalValueSize := 0
		totalSize := 0
		if tree == nil {
			fmt.Printf("Tree does not exist for module %s \n", moduleName)
		} else {
			fmt.Printf("Calculating for module: %s \n", moduleName)
			keySizeByPrefix := map[string]int64{}
			valueSizeByPrefix := map[string]int64{}
			tree.ScanPostOrder(func(node memiavl.Node) bool {
				if node.IsLeaf() {
					totalNumKeys++
					keySize := len(node.Key())
					valueSize := len(node.Value())
					totalKeySize += keySize
					totalValueSize += valueSize
					totalSize += keySize + valueSize
					prefix := fmt.Sprintf("%X", node.Key())
					prefix = prefix[:2]
					keySizeByPrefix[prefix] += int64(keySize)
					valueSizeByPrefix[prefix] += int64(valueSize)
				}
				return true
			})
			fmt.Printf("Module %s total numKeys:%d, total keySize:%d, total valueSize:%d, totalSize: %d \n", moduleName, totalNumKeys, totalKeySize, totalValueSize, totalSize)
			prefixKeyResult, _ := json.MarshalIndent(keySizeByPrefix, "", "  ")
			fmt.Printf("Module %s prefix key size breakdown (bytes): %s \n", moduleName, prefixKeyResult)
			prefixValueResult, _ := json.MarshalIndent(valueSizeByPrefix, "", "  ")
			fmt.Printf("Module %s prefix value size breakdown (bytes): %s \n", moduleName, prefixValueResult)

			// Print top 20 contracts by total size
			numToShow := 20
			if valueSizeByPrefix["03"] > 0 || keySizeByPrefix["03"] > 0 {
				type contractSizeEntry struct {
					Address   string
					KeySize   int64
					ValueSize int64
					TotalSize int64
					KeyCount  int
				}

				contractSizes := make(map[string]*contractSizeEntry)

				// Scan again to collect per-contract statistics
				tree.ScanPostOrder(func(node memiavl.Node) bool {
					if node.IsLeaf() {
						prefix := fmt.Sprintf("%X", node.Key())
						if prefix[:2] == "03" {
							// Extract contract address from key (assuming it follows after "03")
							addr := prefix[2:42] // Adjust indices based on your key format
							if _, exists := contractSizes[addr]; !exists {
								contractSizes[addr] = &contractSizeEntry{Address: addr}
							}
							entry := contractSizes[addr]
							entry.KeySize += int64(len(node.Key()))
							entry.ValueSize += int64(len(node.Value()))
							entry.TotalSize = entry.KeySize + entry.ValueSize
							entry.KeyCount++
						}
					}
					return true
				})

				// Convert map to slice
				var sortedContracts []contractSizeEntry
				for _, entry := range contractSizes {
					sortedContracts = append(sortedContracts, *entry)
				}

				// Sort by total size in descending order
				sort.Slice(sortedContracts, func(i, j int) bool {
					return sortedContracts[i].TotalSize > sortedContracts[j].TotalSize
				})

				fmt.Printf("\nDetailed breakdown for 0x03 prefix (top 20 contracts by total size):\n")
				fmt.Printf("%-42s %15s %15s %15s %10s\n", "Contract Address", "Key Size", "Value Size", "Total Size", "Key Count")
				fmt.Printf("%s\n", strings.Repeat("-", 100))

				if len(sortedContracts) < numToShow {
					numToShow = len(sortedContracts)
				}
				for i := 0; i < numToShow; i++ {
					contract := sortedContracts[i]
					fmt.Printf("0x%-40s %15d %15d %15d %10d\n",
						contract.Address,
						contract.KeySize,
						contract.ValueSize,
						contract.TotalSize,
						contract.KeyCount)
				}
			}
		}
	}
	return nil
}
