package ss

import (
	"bytes"
	"fmt"
	"regexp"

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

	db = dbm.NewPrefixDB(db, []byte("s/k:acc/n"))

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

// Export leaf nodes of iavl
func ExportLeafNodes(db dbm.DB, ch chan<- types.RawSnapshotNode) error {
	// Module by module, TODO: Potentially parallelize
	db = dbm.NewPrefixDB(db, []byte("s/k:acc/n"))

	count := 0
	leafNodeCount := 0
	fmt.Println("Scanning database and exporting leaf nodes...")

	for _, module := range modules {
		fmt.Printf("Iterating through %s module...\n", module)

		db = dbm.NewPrefixDB(db, []byte(buildPrefix(module)))
		itr, err := db.Iterator(nil, nil)
		if err != nil {
			fmt.Printf("error Export Leaf Nodes %+v\n", err)
			return fmt.Errorf("failed to create iterator: %w", err)
		}
		defer itr.Close()

		for ; itr.Valid(); itr.Next() {
			key := bytes.Clone(itr.Key())
			value := bytes.Clone(itr.Value())

			node, err := iavl.MakeNode(value)

			node.GetNodeKey()
			node.GetValue()
			if err != nil {
				return fmt.Errorf("failed to make node: %w", err)
			}

			// leaf node
			if node.GetHeight() == 0 {
				leafNodeCount++
				fmt.Printf("itr.Key %+v itr.Value %+v version %+v\n", string(itr.Key()), string(itr.Value()), node.GetVersion())
				fmt.Printf("leaf node Key %+v Value %+v \n\n", string(node.GetNodeKey()), string(node.GetValue()))
				first, second, err := extractPrefix(key, `^s/[^/]+/[^/]+/n`)
				fmt.Printf("REGEX first %+v second %+v err %+v\n\n", first, second, err)
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

		if err := itr.Error(); err != nil {
			return fmt.Errorf("iterator error: %w", err)
		}

	}

	fmt.Printf("DB contains %d entries, exported %d leaf nodes\n", count, leafNodeCount)
	return nil
}

func extractPrefix(data []byte, prefix string) ([]byte, []byte, error) {
	re := regexp.MustCompile(prefix)
	loc := re.FindIndex(data)

	if loc == nil {
		return nil, data, fmt.Errorf("prefix not found")
	}

	return data[:loc[1]], data[loc[1]:], nil
}

func buildPrefix(moduleName string) string {
	return fmt.Sprintf("s/k:%s/n", moduleName)
}
