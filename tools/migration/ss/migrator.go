package ss

import (
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

// Export leaf nodes of iavl
func ExportLeafNodes(db dbm.DB, ch chan<- types.RawSnapshotNode) error {
	count := 0
	leafNodeCount := 0
	fmt.Println("Scanning database and exporting leaf nodes...")

	itr, err := db.Iterator(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer itr.Close()

	for ; itr.Valid(); itr.Next() {
		key := itr.Key()
		value := itr.Value()

		node, err := iavl.MakeNode(value)
		if err != nil {
			return fmt.Errorf("failed to make node: %w", err)
		}
		if node.GetHeight() == 0 {
			leafNodeCount++
			ch <- types.RawSnapshotNode{
				// TODO: Parse store key properly
				StoreKey: string(key),
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

	fmt.Printf("DB contains %d entries, exported %d leaf nodes\n", count, leafNodeCount)
	return nil
}
