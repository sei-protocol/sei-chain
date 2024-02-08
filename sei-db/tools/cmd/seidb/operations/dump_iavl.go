package operations

import (
	"fmt"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	"github.com/sei-protocol/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

var ALL_MODULES = []string{
	"dex", "wasm", "aclaccesscontrol", "oracle", "epoch", "mint", "acc", "bank", "crisis", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
}

func DumpIAVLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-iavl",
		Short: "Iterate and dump memIAVL data",
		Run:   execute,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	cmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	cmd.PersistentFlags().Int64("height", 0, "Block Height")
	cmd.PersistentFlags().StringP("module", "m", "", "Module to export. Default to export all")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
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

	err := DumpIAVLData(module, dbDir, outputDir, height)
	if err != nil {
		panic(err)
	}
}

// DumpIAVLData print the raw keys and values for given module at given height for memIAVL tree
func DumpIAVLData(module string, dbDir string, outputDir string, height int64) error {
	opts := memiavl.Options{
		Dir:             dbDir,
		ZeroCopy:        true,
		CreateIfMissing: false,
	}
	db, err := memiavl.OpenDB(logger.NewNopLogger(), height, opts)
	if err != nil {
		return err
	}
	defer db.Close()
	modules := []string{}
	if module == "" {
		modules = ALL_MODULES
	} else {
		modules = append(modules, module)
	}

	for _, moduleName := range modules {
		fmt.Printf("Dumping module: %s \n", moduleName)
		currentFile, err := utils.CreateFile(outputDir, moduleName)
		if err != nil {
			return err
		}
		tree := db.TreeByName(module)
		if tree == nil {
			fmt.Printf("Tree does not exist for module %s \n", moduleName)
			continue
		} else {
			_, err := currentFile.WriteString(fmt.Sprintf("Tree %s has version %d and root hash: %X \n", moduleName, tree.Version(), tree.RootHash()))
			if err != nil {
				return nil
			}
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
	return nil
}
