package ss

import (
	"bytes"
	"fmt"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/sei-protocol/sei-db/ss"
	dbm "github.com/tendermint/tm-db"
)

type Migrator struct {
	iavlDB     dbm.DB
	stateStore types.StateStore
}

// TODO: make this configurable?
const (
	DefaultCacheSize int = 10000
)

var modules = []string{
	"wasm", "aclaccesscontrol", "oracle", "epoch", "mint", "acc", "bank", "crisis", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
}

func NewMigrator(homeDir string, db dbm.DB) *Migrator {
	// TODO: Pass in more configs outside default, in particular ImportNumWorkers
	ssConfig := config.DefaultStateStoreConfig()
	ssConfig.Enable = true

	stateStore, err := ss.NewStateStore(log.NewNopLogger(), homeDir, ssConfig)
	if err != nil {
		panic(err)
	}

	return &Migrator{
		iavlDB:     db,
		stateStore: stateStore,
	}
}

func (m *Migrator) Migrate(version int64, homeDir string) error {
	// TODO: Set earliest / latest version
	// TODO: Read in capacity of this buffered channel as param
	ch := make(chan types.RawSnapshotNode, 1000)
	errCh := make(chan error, 2)

	fmt.Println("Beginning Migration...")

	// Goroutine to iterate through iavl and export leaf nodes
	go func() {
		defer close(ch)
		errCh <- ExportLeafNodes(m.iavlDB, ch)
	}()

	go func() {
		errCh <- m.stateStore.RawImport(ch)
	}()

	// Block on completion of both goroutines
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	return nil
}

func (m *Migrator) Verify(version int64, homeDir string) error {
	// TODO: Call into Read Tree and run pebbleDB Get() for each key
	// Do vice versa as well?
	var verifyErr error
	for _, module := range modules {
		tree, err := ReadTree(m.iavlDB, version, []byte(buildTreePrefix(module)))
		if err != nil {
			fmt.Printf("Error Read Tree: %s", err.Error())
			return err
		}
		tree.Iterate(func(key []byte, value []byte) bool {
			// Run Get against PebbleDB
			val, err := m.stateStore.Get(module, version, key)
			fmt.Printf("Verify Tree Iterate: key %s value %s pebbledb val %s\n", string(key), string(value), string(val))
			if err != nil {
				verifyErr = fmt.Errorf("verificatino error: error retrieving key %s with err %w", string(key), err)
				return true
			}
			if val == nil {
				verifyErr = fmt.Errorf("verification error: Key %s does not exist in state store", string(key))
				return true
			}
			return false
		})
	}
	return verifyErr
}

// Export leaf nodes of iavl
func ExportLeafNodes(db dbm.DB, ch chan<- types.RawSnapshotNode) error {
	// Module by module, TODO: Potentially parallelize
	count := 0
	leafNodeCount := 0
	fmt.Println("Scanning database and exporting leaf nodes...")

	for _, module := range modules {
		fmt.Printf("Iterating through %s module...\n", module)

		// Can't use the previous, have to create an inner
		prefixDB := dbm.NewPrefixDB(db, []byte(buildRawPrefix(module)))
		itr, err := prefixDB.Iterator(nil, nil)
		if err != nil {
			fmt.Printf("error Export Leaf Nodes %+v\n", err)
			return fmt.Errorf("failed to create iterator: %w", err)
		}
		defer itr.Close()

		for ; itr.Valid(); itr.Next() {
			value := bytes.Clone(itr.Value())

			node, err := iavl.MakeNode(value)

			if err != nil {
				fmt.Printf("failed to make node err: %+v\n", err)
				return fmt.Errorf("failed to make node: %w", err)
			}

			// leaf node
			if node.GetHeight() == 0 {
				leafNodeCount++
				ch <- types.RawSnapshotNode{
					// TODO: Likely need to clone
					StoreKey: module,
					Key:      node.GetNodeKey(),
					Value:    node.GetValue(),
					Version:  node.GetVersion(),
				}
			}

			count++
			if count%10000 == 0 {
				fmt.Printf("Total scanned: %d, leaf nodes exported: %d\n", count, leafNodeCount)
			}
		}

		fmt.Printf("Finished scanning module %s Total scanned: %d, leaf nodes exported: %d\n", module, count, leafNodeCount)

		if err := itr.Error(); err != nil {
			fmt.Printf("iterator error: %+v\n", err)
			return fmt.Errorf("iterator error: %w", err)
		}

	}

	fmt.Printf("DB contains %d entries, exported %d leaf nodes\n", count, leafNodeCount)
	return nil
}

func buildRawPrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/n", moduleName)
}

func buildTreePrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/", moduleName)
}

func ReadTree(db dbm.DB, version int64, prefix []byte) (*iavl.MutableTree, error) {
	// TODO: Verify if we need a prefix here (or can just iterate through all modules)
	if len(prefix) != 0 {
		db = dbm.NewPrefixDB(db, prefix)
	}

	tree, err := iavl.NewMutableTree(db, DefaultCacheSize, true)
	if err != nil {
		return nil, err
	}
	ver, err := tree.LoadVersion(version)
	fmt.Printf("Got version: %d\n", ver)
	return tree, err
}
