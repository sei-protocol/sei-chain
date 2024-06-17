package operations

import (
	"encoding/json"
	"fmt"

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
			sizeByPrefix := map[string]int{}
			tree.ScanPostOrder(func(node memiavl.Node) bool {
				if node.IsLeaf() {
					totalNumKeys++
					totalKeySize += len(node.Key())
					totalValueSize += len(node.Value())
					totalSize += len(node.Key()) + len(node.Value())
					prefix := fmt.Sprintf("%X", node.Key())
					prefix = prefix[:2]
					sizeByPrefix[prefix] += len(node.Value())
				}
				return true
			})
			fmt.Printf("Module %s total numKeys:%d, total keySize:%d, total valueSize:%d, totalSize: %d \n", moduleName, totalNumKeys, totalKeySize, totalValueSize, totalSize)
			result, _ := json.MarshalIndent(sizeByPrefix, "", "  ")
			fmt.Printf("Module %s prefix breakdown: %s \n", moduleName, result)
		}
	}
	return nil
}
