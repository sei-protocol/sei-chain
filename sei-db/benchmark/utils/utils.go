package utils

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/iavl"
)

const (
	DefaultCacheSize int = 10000
)

type KeyValuePair struct {
	Key   []byte `json:"key"`
	Value []byte `json:"value"`
}

// Opens application db
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
	_, err = tree.LoadVersion(int64(version))
	return tree, err
}

// Writes raw key / values from a tree to a file
func WriteTreeDataToFile(tree *iavl.MutableTree, filename string) {
	f, err := os.Create(filename)
	if err != nil {
		panic(err)
	}

	defer f.Close()
	tree.Iterate(func(key []byte, value []byte) bool {
		if err := writeByteSlice(f, key); err != nil {
			panic(err)
		}
		if err := writeByteSlice(f, value); err != nil {
			panic(err)
		}
		return false
	})
}

// Writes raw bytes to file
func writeByteSlice(w io.Writer, data []byte) error {
	length := uint32(len(data))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// Reads raw keys / values from a file
// TODO: Adding in ability to chunk larger exported file (like for wasm dir)
func ReadKVEntriesFromFile(filename string) ([]KeyValuePair, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var kvPairs []KeyValuePair
	for {
		key, err := readByteSlice(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		value, err := readByteSlice(f)
		if err != nil {
			return nil, err
		}

		kvPairs = append(kvPairs, KeyValuePair{Key: key, Value: value})
	}

	return kvPairs, nil
}

func readByteSlice(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	data := make([]byte, length)
	_, err := io.ReadFull(r, data)
	return data, err
}

// Randomly Shuffle kv pairs once read
func RandomShuffle(kvPairs []KeyValuePair) {
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(kvPairs), func(i, j int) {
		kvPairs[i], kvPairs[j] = kvPairs[j], kvPairs[i]
	})
}

// Add Random Bytes to keys / values
func AddRandomBytes(data []byte, numBytes int) []byte {
	randomBytes := make([]byte, numBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		panic(fmt.Sprintf("Failed to generate random bytes: %v", err))
	}
	return append(data, randomBytes...)
}

// NOTE: Assumes latencies is sorted
func CalculatePercentile(latencies []time.Duration, percentile float64) time.Duration {
	if percentile < 0 || percentile > 100 {
		panic(fmt.Sprintf("Invalid percentile: %f", percentile))
	}
	index := int(float64(len(latencies)-1) * percentile / 100.0)
	return latencies[index]
}
