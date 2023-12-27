package benchmark

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/sei-db/tools/utils"
	"github.com/spf13/cobra"
)

func GenerateCmd() *cobra.Command {
	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate uses the iavl viewer logic to write out the raw keys and values from the kb for each module",
		Run:   generate,
	}

	generateCmd.PersistentFlags().StringP("leveldb-dir", "l", "/root/.sei/data/application.db", "Level db dir")
	generateCmd.PersistentFlags().StringP("output-dir", "o", "", "Output Directory")
	generateCmd.PersistentFlags().StringP("modules", "m", "", "Comma separated modules to export")
	generateCmd.PersistentFlags().IntP("version", "v", 0, "Database Version")
	generateCmd.PersistentFlags().IntP("chunk-size", "c", 1000, "KV File Chunk Size")

	return generateCmd
}

func generate(cmd *cobra.Command, _ []string) {
	levelDBDir, _ := cmd.Flags().GetString("leveldb-dir")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	modules, _ := cmd.Flags().GetString("modules")
	version, _ := cmd.Flags().GetInt("version")
	chunkSize, _ := cmd.Flags().GetInt("chunk-size")

	if outputDir == "" {
		panic("Must provide output dir when generating raw kv data")
	}

	// Default to all modules
	exportModules := []string{
		"dex", "wasm", "accesscontrol", "oracle", "epoch", "mint", "acc", "bank", "crisis", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
	}
	if modules != "" {
		exportModules = strings.Split(modules, ",")
	}
	GenerateData(levelDBDir, exportModules, outputDir, version, chunkSize)
}

// Outputs the raw keys and values for all modules at a height to a file
func GenerateData(dbDir string, modules []string, outputDir string, version int, chunkSize int) {
	// Create output directory
	err := os.MkdirAll(outputDir, fs.ModePerm)
	if err != nil {
		panic(err)
	}

	// Generate raw kv data for each module
	db, err := utils.OpenDB(dbDir)
	if err != nil {
		panic(err)
	}
	for _, module := range modules {
		fmt.Printf("Generating Raw Keys and Values for %s module at version %d\n", module, version)

		modulePrefix := fmt.Sprintf("s/k:%s/", module)
		tree, err := utils.ReadTree(db, version, []byte(modulePrefix))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading data: %s\n", err)
			return
		}
		treeHash, err := tree.Hash()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing tree: %s\n", err)
			return
		}

		fmt.Printf("Tree hash is %X, tree size is %d\n", treeHash, tree.ImmutableTree().Size())

		outputFileNamePattern := filepath.Join(outputDir, module)
		utils.WriteTreeDataToFile(tree, outputFileNamePattern, chunkSize)
	}
}
