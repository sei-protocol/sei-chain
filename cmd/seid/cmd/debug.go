package cmd

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/iavl"
	"github.com/spf13/cobra"
	dbm "github.com/tendermint/tm-db"
)

const (
	DefaultCacheSize int    = 10000
	FlagDBPath       string = "db-path"
	FlagOutputDir    string = "output-dir"
	FlagModuleName   string = "module"
)

var modules = []string{
	"wasm", "aclaccesscontrol", "oracle", "epoch", "mint", "acc", "bank", "crisis", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
}

func DumpIavlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump-iavl [height]",
		Short: "Dump iavl data for a specific height",
		Long: fmt.Sprintf(`Dump iavl data for a specific height

Example:
$ %s debug dump-iavl 12345
			`, version.AppName),
		Args: cobra.ExactArgs(1),
		RunE: dumpIavlCmdHandler,
	}

	cmd.Flags().String(FlagOutputDir, "", "The output directory for the iavl dump, if none specified, the home directory will be used")
	cmd.Flags().StringP(FlagDBPath, "d", "", "The path to the db, default is $HOME/.sei/data/application.db")
	cmd.Flags().StringP(FlagModuleName, "m", "", "The specific module to dump IAVL for, if none specified, all modules will be dumped")

	return cmd
}

//nolint:gosec
func dumpIavlCmdHandler(cmd *cobra.Command, args []string) error {
	var err error
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dbPath, err := cmd.Flags().GetString(FlagDBPath)
	if err != nil {
		return err
	}
	if dbPath == "" {
		dbPath = fmt.Sprintf("%s/.sei/data/application.db", home)
	}

	fmt.Printf("db path: %s\n", dbPath)

	version := 0
	version, err = strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid version number: %s\n", err)
		os.Exit(1)
	}
	// create output directory OR take in arg
	outputDir, err := cmd.Flags().GetString(FlagOutputDir)
	if err != nil {
		return err
	}
	if outputDir == "" {
		outputDir = fmt.Sprintf("%s/state_%s/", home, args[0])
	}
	outputZipPath := strings.TrimRight(outputDir, "/") + ".zip"
	err = os.Mkdir(outputDir, os.ModePerm)
	if err != nil {
		return err
	}

	moduleName, err := cmd.Flags().GetString(FlagModuleName)
	if err != nil {
		return err
	}

	if moduleName != "" {
		// if module name passed in, override `modules`
		modules = []string{moduleName}
	}
	db, err := OpenDB(dbPath)
	if err != nil {
		return err
	}
	for _, module := range modules {
		fmt.Printf("Processing Module: %s\n", module)
		tree, err := ReadTree(db, version, []byte(BuildPrefix(module)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading data: %s\n", err)
			continue
			// os.Exit(1)
		}
		parser := ModuleParserMap[module]
		lines := PrintKeys(tree, parser)
		hash, err := tree.Hash()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing tree: %s\n", err)
			os.Exit(1)
		}
		lines = append(lines, []byte(fmt.Sprintf("Hash: %X\n", hash))...)
		lines = append(lines, []byte(fmt.Sprintf("Size: %X\n", tree.ITree.Size()))...)
		// write lines to file
		err = os.WriteFile(fmt.Sprintf("%s/%s.data", outputDir, module), lines, os.ModePerm)
		if err != nil {
			return err
		}

		shapeLines, err := PrintShape(tree)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error building tree shape: %s\n", err)
			os.Exit(1)
		}
		// write lines to file
		err = os.WriteFile(fmt.Sprintf("%s/%s.shape", outputDir, module), shapeLines, os.ModePerm)
		if err != nil {
			return err
		}
	}
	destinationFile, err := os.Create(outputZipPath)
	if err != nil {
		return err
	}
	outputZip := zip.NewWriter(destinationFile)
	err = filepath.Walk(outputDir, func(filePath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if err != nil {
			return err
		}
		relPath := strings.TrimPrefix(filePath, filepath.Dir(outputZipPath))
		zipFile, err := outputZip.Create(relPath)
		if err != nil {
			return err
		}
		fsFile, err := os.Open(filePath)
		if err != nil {
			return err
		}
		_, err = io.Copy(zipFile, fsFile)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	err = outputZip.Close()
	if err != nil {
		return err
	}
	return nil
}

func BuildPrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/", moduleName)
}

func OpenDB(dir string) (dbm.DB, error) {
	switch {
	case strings.HasSuffix(dir, ".db"):
		dir = dir[:len(dir)-3]
	case strings.HasSuffix(dir, ".db/"):
		dir = dir[:len(dir)-4]
	default:
		return nil, fmt.Errorf("database directory must end with .db")
	}

	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// TODO: doesn't work on windows!
	cut := strings.LastIndex(dir, "/")
	if cut == -1 {
		return nil, fmt.Errorf("cannot cut paths on %s", dir)
	}
	name := dir[cut+1:]
	db, err := dbm.NewGoLevelDB(name, dir[:cut])
	if err != nil {
		return nil, err
	}
	return db, nil
}

// ReadTree loads an iavl tree from the directory
// If version is 0, load latest, otherwise, load named version
// The prefix represents which iavl tree you want to read. The iaviwer will always set a prefix.
func ReadTree(db dbm.DB, version int, prefix []byte) (*iavl.MutableTree, error) {
	if len(prefix) != 0 {
		db = dbm.NewPrefixDB(db, prefix)
	}

	tree, err := iavl.NewMutableTree(db, DefaultCacheSize, true)
	if err != nil {
		return nil, err
	}
	ver, err := tree.LoadVersion(int64(version))
	fmt.Printf("Got version: %d\n", ver)
	return tree, err
}

func PrintKeys(tree *iavl.MutableTree, moduleParser ModuleParser) []byte {
	fmt.Println("Printing all keys with hashed values (to detect diff)")
	if moduleParser != nil {
		fmt.Println("Parsing module with human readable keys")
	}
	lines := []byte{}
	tree.Iterate(func(key []byte, value []byte) bool { //nolint:errcheck
		printKey := parseWeaveKey(key)
		// parse key if we have a parser
		if moduleParser != nil {
			parsed, err := moduleParser(key)
			if err != nil {
				printKey = strings.Join([]string{printKey, err.Error()}, " | ")
			} else {
				printKey = strings.Join(append([]string{printKey}, parsed...), " | ")
			}
		}
		digest := sha256.Sum256(value)
		lines = append(lines, []byte(fmt.Sprintf("  %s\n    %X\n", printKey, digest))...)
		return false
	})
	return lines
}

// parseWeaveKey assumes a separating : where all in front should be ascii,
// and all afterwards may be ascii or binary
func parseWeaveKey(key []byte) string {
	cut := bytes.IndexRune(key, ':')
	if cut == -1 {
		return encodeID(key)
	}
	prefix := key[:cut]
	id := key[cut+1:]
	return fmt.Sprintf("%s:%s", encodeID(prefix), encodeID(id))
}

// casts to a string if it is printable ascii, hex-encodes otherwise
func encodeID(id []byte) string {
	for _, b := range id {
		if b < 0x20 || b >= 0x80 {
			return strings.ToUpper(hex.EncodeToString(id))
		}
	}
	return string(id)
}

func PrintShape(tree *iavl.MutableTree) ([]byte, error) {
	// shape := tree.RenderShape("  ", nil)
	shape, err := tree.ITree.RenderShape("  ", nodeEncoder)
	if err != nil {
		return []byte{}, err
	}
	return []byte(strings.Join(shape, "\n")), nil
}

func nodeEncoder(id []byte, depth int, isLeaf bool) string {
	prefix := fmt.Sprintf("-%d ", depth)
	if isLeaf {
		prefix = fmt.Sprintf("*%d ", depth)
	}
	if len(id) == 0 {
		return fmt.Sprintf("%s<nil>", prefix)
	}
	return fmt.Sprintf("%s%s", prefix, parseWeaveKey(id))
}

func PrintVersions(tree *iavl.MutableTree) {
	versions := tree.AvailableVersions()
	fmt.Println("Available versions:")
	for _, v := range versions {
		fmt.Printf("  %d\n", v)
	}
}
