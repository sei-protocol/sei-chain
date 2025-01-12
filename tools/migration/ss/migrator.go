package ss

import (
	"bytes"
	"fmt"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/tools/migration/utils"
	"github.com/sei-protocol/sei-db/ss/types"
	dbm "github.com/tendermint/tm-db"
)

type Migrator struct {
	iavlDB        dbm.DB
	stateStore    types.StateStore
	oldStateStore types.StateStore
}

// TODO: make this configurable?
const (
	DefaultCacheSize int = 10000
)

func NewMigrator(db dbm.DB, stateStore types.StateStore, oldStateStore types.StateStore) *Migrator {
	return &Migrator{
		iavlDB:        db,
		stateStore:    stateStore,
		oldStateStore: oldStateStore,
	}
}

func (m *Migrator) Migrate(version int64, homeDir string) error {
	// Channel to send RawSnapshotNodes to the importer.
	ch := make(chan types.RawSnapshotNode, 10000)
	// Channel to capture errors from both goroutines below.
	errCh := make(chan error, 2)

	fmt.Printf("Starting migration for 'distribution' module from version\n")

	// Goroutine #1: Export distribution leaf nodes into ch
	go func() {
		defer close(ch)
		errCh <- exportDistributionLeafNodes(m.oldStateStore, m.stateStore, ch)
	}()

	// go func() {
	// 	errCh <- m.stateStore.RawImport(ch)
	// }()

	// Wait for both goroutines to complete
	for i := 0; i < 1; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	return nil
}

func exportDistributionLeafNodes(
	oldStateStore types.StateStore,
	newStateStore types.StateStore,
	ch chan<- types.RawSnapshotNode,
) error {

	var totalExported int
	startTime := time.Now()

	// RawIterate will scan *all* keys in the "distribution" store.
	// We'll filter them by version in the callback.
	var misMatch int
	stop, err := oldStateStore.RawIterate("distribution", func(key, value []byte, version int64) bool {
		totalExported++
		valBz, err := newStateStore.Get("distribution", version, key)
		if err != nil {
			panic(err)
		}
		if value != nil && valBz != nil && !bytes.Equal(valBz, value) {
			misMatch++
			fmt.Printf("Value mismatch for key %s: expected %s, got %s\n", string(key), string(value), string(valBz))
		}
		if misMatch > 3 {
			panic("3 mismatches")
		}
		// ch <- types.RawSnapshotNode{
		// 	StoreKey: "distribution",
		// 	Key:      key,
		// 	Value:    value,
		// 	Version:  version,
		// }

		// Optional progress logging every 1,000,000 keys:
		if totalExported%1_000_000 == 0 {
			fmt.Printf("[SingleWorker][%s] Verified %d distribution keys so far\n",
				time.Now().Format(time.RFC3339), totalExported,
			)
		}
		// Return false to continue iterating
		return false
	})
	if err != nil {
		return fmt.Errorf("RawIterate error: %w", err)
	}
	if stop {
		fmt.Printf("[SingleWorker][%s] Iteration stopped early; callback returned true at some point.\n",
			time.Now().Format(time.RFC3339),
		)
	}

	fmt.Printf(
		"[%s] Completed exporting distribution store for versions. Total keys: %d. Duration: %s\n",
		time.Now().Format(time.RFC3339), totalExported, time.Since(startTime),
	)
	fmt.Printf("Finished export at %s\n", time.Now().Format(time.RFC3339))
	return nil
}

func (m *Migrator) Verify(version int64) error {
	var verifyErr error
	for _, module := range utils.Modules {
		tree, err := ReadTree(m.iavlDB, version, []byte(utils.BuildTreePrefix(module)))
		if err != nil {
			fmt.Printf("Error reading tree %s: %s\n", module, err.Error())
			return err
		}
		var count int
		_, err = tree.Iterate(func(key []byte, value []byte) bool {
			// Run Get() against PebbleDB and verify values match
			val, err := m.stateStore.Get(module, version, key)
			if err != nil {
				verifyErr = fmt.Errorf("verification error: error retrieving key %s with err %w", string(key), err)
				return true
			}
			if val == nil {
				verifyErr = fmt.Errorf("verification error: Key %s does not exist in state store", string(key))
				return true
			}
			if !bytes.Equal(val, value) {
				verifyErr = fmt.Errorf("verification error: value doesn't match for key %s. Expected %s, got %s", string(key), string(value), string(val))
				return true
			}
			count++
			if count%1000000 == 0 {
				fmt.Printf("SeiDB Archive Migration: Verified %d keys in for module %s\n", count, module)
			}
			return false
		})
		if err != nil {
			fmt.Printf("SeiDB Archive Migration: Failed to iterate the tree %s: %s\n", module, err.Error())
			return err
		}
		fmt.Printf("SeiDB Archive Migration:: Finished verifying module %s, total scanned: %d keys\n", module, count)
	}
	return verifyErr
}

func ExportLeafNodesFromKey(db dbm.DB, ch chan<- types.RawSnapshotNode, startKey []byte, startModule string) error {
	count := 0
	leafNodeCount := 0
	fmt.Println("SeiDB Archive Migration: Scanning database and exporting leaf nodes...")

	startTimeTotal := time.Now() // Start measuring total time

	var batchLeafNodeCount int
	startModuleFound := startModule == "" // true if no start module specified

	for _, module := range utils.Modules {
		if !startModuleFound {
			if module == startModule {
				startModuleFound = true
			} else {
				continue
			}
		}
		startTimeModule := time.Now() // Measure time for each module
		fmt.Printf("SeiDB Archive Migration: Iterating through %s module...\n", module)

		prefixDB := dbm.NewPrefixDB(db, []byte(utils.BuildRawPrefix(module)))
		var itr dbm.Iterator
		var err error

		// If there is a starting key, seek to it, otherwise start from the beginning
		if startKey != nil && bytes.HasPrefix(startKey, []byte(utils.BuildRawPrefix(module))) {
			itr, err = prefixDB.Iterator(startKey, nil) // Start from the latest key
		} else {
			itr, err = prefixDB.Iterator(nil, nil) // Start from the beginning
		}

		if err != nil {
			fmt.Printf("SeiDB Archive Migration: Error creating iterator: %+v\n", err)
			return fmt.Errorf("failed to create iterator: %w", err)
		}
		defer itr.Close()

		startTimeBatch := time.Now() // Measure time for every 10,000 iterations

		for ; itr.Valid(); itr.Next() {
			value := bytes.Clone(itr.Value())

			node, err := iavl.MakeNode(value)
			if err != nil {
				fmt.Printf("SeiDB Archive Migration: Failed to make node: %+v\n", err)
				return fmt.Errorf("failed to make node: %w", err)
			}

			// Only export leaf nodes
			if node.GetHeight() == 0 {
				leafNodeCount++
				batchLeafNodeCount++
				ch <- types.RawSnapshotNode{
					StoreKey: module,
					Key:      node.GetNodeKey(),
					Value:    node.GetValue(),
					Version:  node.GetVersion(),
				}
			}

			count++
			if count%1000000 == 0 {
				batchDuration := time.Since(startTimeBatch)
				fmt.Printf("SeiDB Archive Migration: Last 1,000,000 iterations took: %v. Total scanned: %d, leaf nodes exported: %d\n", batchDuration, count, leafNodeCount)
				metrics.IncrCounterWithLabels([]string{"sei", "migration", "leaf_nodes_exported"}, float32(batchLeafNodeCount), []metrics.Label{
					{Name: "module", Value: module},
				})

				batchLeafNodeCount = 0
				startTimeBatch = time.Now()
			}
		}

		if err := itr.Error(); err != nil {
			fmt.Printf("Iterator error: %+v\n", err)
			return fmt.Errorf("iterator error: %w", err)
		}

		moduleDuration := time.Since(startTimeModule)
		fmt.Printf("SeiDB Archive Migration: Finished scanning module %s. Time taken: %v. Total scanned: %d, leaf nodes exported: %d\n", module, moduleDuration, count, leafNodeCount)
	}

	totalDuration := time.Since(startTimeTotal)
	fmt.Printf("SeiDB Archive Migration: DB scanning completed. Total time taken: %v. Total entries scanned: %d, leaf nodes exported: %d\n", totalDuration, count, leafNodeCount)

	return nil
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
	_, err = tree.LoadVersion(version)
	return tree, err
}
