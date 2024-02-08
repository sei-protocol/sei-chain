package operations

import (
	"fmt"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/sc/memiavl"
	"github.com/spf13/cobra"
)

func DumpIAVLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-iavl",
		Short: "Iterate and dump memIAVL data and shape",
		Run:   execute,
	}

	cmd.PersistentFlags().StringP("db-dir", "d", "", "Database Directory")
	cmd.PersistentFlags().Int64("height", 0, "Block Height")
	cmd.PersistentFlags().StringP("module", "m", "", "Module to export. Default to export all")
	return cmd
}

func execute(cmd *cobra.Command, _ []string) {
	module, _ := cmd.Flags().GetString("module")
	dbDir, _ := cmd.Flags().GetString("db-dir")
	height, _ := cmd.Flags().GetInt64("height")

	if dbDir == "" {
		panic("Must provide database dir")
	}
	if module == "" {
		panic("Must provide module")
	}

	DumpIAVLData(module, dbDir, height)
}

// DumpIAVLData print the raw keys and values for given module at given height for memIAVL tree
func DumpIAVLData(module string, dbDir string, height int64) {
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
	tree := db.TreeByName(module)
	if tree == nil {
		panic(fmt.Sprintf("Tree does not exist for module %s", module))
	} else {
		fmt.Printf("Tree %s has version %d and root hash: %X \n", module, tree.Version(), tree.RootHash())
	}
	tree.ScanPostOrder(func(node memiavl.Node) bool {
		if node.IsLeaf() {
			fmt.Printf("Key: %X, Value: %X \n", node.Key(), node.Value())
		}
		return true
	})

}
