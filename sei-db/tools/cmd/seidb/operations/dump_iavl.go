package operations

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func DumpIAVLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-iavl",
		Short: "Iterate and dump memIAVL data",
		Run:   executeDumpIAVL,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	cmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	cmd.PersistentFlags().Int64("height", 0, "Block Height")
	cmd.PersistentFlags().StringP("module", "m", "", "Module to export. Default to export all")
	return cmd
}

func executeDumpIAVL(cmd *cobra.Command, _ []string) {
	module, _ := cmd.Flags().GetString("module")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	height, _ := cmd.Flags().GetInt64("height")

	if dbDir == "" {
		panic("Must provide database dir")
	}

	if outputDir == "" {
		panic("Must provide output dir")
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
	err = DumpIAVLData(module, db, outputDir)
	if err != nil {
		panic(err)
	}
}

// DumpIAVLData print the raw keys and values for given module at given height for memIAVL tree
func DumpIAVLData(module string, db *memiavl.DB, outputDir string) error {
	modules := []string{}
	if module == "" {
		modules = AllModules
	} else {
		modules = append(modules, module)
	}

	for _, moduleName := range modules {
		tree := db.TreeByName(moduleName)
		if tree == nil {
			fmt.Printf("Tree does not exist for module %s \n", moduleName)
		} else {
			fmt.Printf("Dumping module: %s \n", moduleName)
			currentFile, err := utils.CreateFile(outputDir, moduleName)
			if err != nil {
				return err
			}
			_, err = currentFile.WriteString(fmt.Sprintf("Tree %s has version %d and root hash: %X \n", moduleName, tree.Version(), tree.RootHash()))
			if err != nil {
				return nil
			}
			tree.ScanPostOrder(func(node memiavl.Node) bool {
				if node.IsLeaf() {
					_, err := currentFile.WriteString(fmt.Sprintf("Key: %X, Value: %X \n", node.Key(), node.Value()))
					if err != nil {
						panic(err)
					}
				}
				return true
			})
			currentFile.Close()
			fmt.Printf("Finished dumping module: %s \n", moduleName)
		}
	}
	return nil
}
