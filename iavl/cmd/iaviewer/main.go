package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/iavl"
	ibytes "github.com/sei-protocol/sei-chain/iavl/internal/bytes"
)

// TODO: make this configurable?
const (
	DefaultCacheSize int = 10000
)

func main() {
	args := os.Args[1:]
	if len(args) < 3 || (args[0] != "data" && args[0] != "keys" && args[0] != "shape" && args[0] != "versions" && args[0] != "size") {
		fmt.Fprintln(os.Stderr, "Usage: iaviewer <data|keys|shape|versions|size> <leveldb dir> <prefix> [version number]")
		fmt.Fprintln(os.Stderr, "<prefix> is the prefix of db, and the iavl tree of different modules in cosmos-sdk uses ")
		fmt.Fprintln(os.Stderr, "different <prefix> to identify, just like \"s/k:gov/\" represents the prefix of gov module")
		os.Exit(1)
	}

	version := 0
	if len(args) == 4 {
		var err error
		version, err = strconv.Atoi(args[3])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid version number: %s\n", err)
			os.Exit(1)
		}
	}

	tree, err := ReadTree(args[1], version, []byte(args[2]))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading data: %s\n", err)
		os.Exit(1)
	}
	treeHash, err := tree.Hash()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error hashing tree: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("Tree hash is %X, tree size is %d\n", treeHash, tree.ImmutableTree().Size())

	switch args[0] {
	case "data":
		PrintTreeData(tree, false)
	case "keys":
		PrintTreeData(tree, true)
	case "shape":
		PrintShape(tree)
	case "versions":
		PrintVersions(tree)
	case "size":
		PrintSize(tree)
	}
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

// nolint: deadcode
func PrintDBStats(db dbm.DB) {
	count := 0
	prefix := map[string]int{}
	itr, err := db.Iterator(nil, nil)
	if err != nil {
		panic(err)
	}

	defer itr.Close()
	for ; itr.Valid(); itr.Next() {
		key := ibytes.UnsafeBytesToStr(itr.Key()[:1])
		prefix[key]++
		count++
	}
	if err := itr.Error(); err != nil {
		panic(err)
	}
	fmt.Printf("DB contains %d entries\n", count)
	for k, v := range prefix {
		fmt.Printf("  %s: %d\n", k, v)
	}
}

// ReadTree loads an iavl tree from the directory
// If version is 0, load latest, otherwise, load named version
// The prefix represents which iavl tree you want to read. The iaviwer will always set a prefix.
func ReadTree(dir string, version int, prefix []byte) (*iavl.MutableTree, error) {
	db, err := OpenDB(dir)
	if err != nil {
		return nil, err
	}
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

func PrintTreeData(tree *iavl.MutableTree, keysOnly bool) {
	fmt.Println("Printing all keys with hashed values (to detect diff)")
	totalKeySize := 0
	totalValSize := 0
	totalNumKeys := 0
	keyPrefixMap := map[string]int{}
	tree.Iterate(func(key []byte, value []byte) bool {
		printKey := parseWeaveKey(key)
		if keysOnly {
			fmt.Printf("%s\n", printKey)
		} else {
			digest := sha256.Sum256(value)
			fmt.Printf("%s\n    %X\n", printKey, digest)
		}
		totalKeySize += len(key)
		totalValSize += len(value)
		totalNumKeys++
		keyPrefixMap[fmt.Sprintf("%x", key[0])]++
		return false
	})
	fmt.Printf("Total key count %d, total key bytes %d, total value bytes %d, prefix map %v\n", totalNumKeys, totalKeySize, totalValSize, keyPrefixMap)
}

// parseWeaveKey assumes a separating : where all in front should be ascii,
// and all afterwards may be ascii or binary
func parseWeaveKey(key []byte) string {
	return encodeID(key)
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

func PrintShape(tree *iavl.MutableTree) {
	// shape := tree.RenderShape("  ", nil)
	//TODO: handle this error
	shape, _ := tree.ImmutableTree().RenderShape("  ", nodeEncoder)
	fmt.Println(strings.Join(shape, "\n"))
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

func PrintSize(tree *iavl.MutableTree) {
	count, totalKeySize, totalValueSize := 0, 0, 0
	keySizeByPrefix, valSizeByPrefix := map[byte]int{}, map[byte]int{}
	tree.Iterate(func(key []byte, value []byte) bool {
		count += 1
		totalKeySize += len(key)
		totalValueSize += len(value)
		if _, ok := keySizeByPrefix[key[0]]; !ok {
			keySizeByPrefix[key[0]] = 0
			valSizeByPrefix[key[0]] = 0
		}
		keySizeByPrefix[key[0]] += len(key)
		valSizeByPrefix[key[0]] += len(value)
		return false
	})
	fmt.Printf("Total entry count: %d. Total key bytes: %d. Total value bytes: %d\n", count, totalKeySize, totalValueSize)
	for p := range keySizeByPrefix {
		fmt.Printf("prefix %d has key bytes %d and value bytes %d\n", p, keySizeByPrefix[p], valSizeByPrefix[p])
	}
}
