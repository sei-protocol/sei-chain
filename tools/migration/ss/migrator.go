package ss

import (
	"bytes"
	"fmt"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/ss/types"
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
	"wasm", "aclaccesscontrol", "oracle", "epoch", "mint", "acc", "bank", "feegrant", "staking", "distribution", "slashing", "gov", "params", "ibc", "upgrade", "evidence", "transfer", "tokenfactory",
}

func NewMigrator(homeDir string, db dbm.DB, stateStore types.StateStore) *Migrator {
	return &Migrator{
		iavlDB:     db,
		stateStore: stateStore,
	}
}

func (m *Migrator) Migrate(version int64, homeDir string) error {
	ch := make(chan types.RawSnapshotNode, 1000)
	errCh := make(chan error, 2)

	// Get the latest key, if any, to resume from
	latestKey, err := m.stateStore.GetLatestMigratedKey()
	if err != nil {
		return fmt.Errorf("failed to get latest key: %w", err)
	}

	latestModule, err := m.stateStore.GetLatestMigratedModule()
	if err != nil {
		return fmt.Errorf("failed to get latest module: %w", err)
	}

	fmt.Println("Starting migration...")

	// Goroutine to iterate through IAVL and export leaf nodes
	go func() {
		defer close(ch)
		errCh <- ExportLeafNodesFromKey(m.iavlDB, ch, latestKey, latestModule)
	}()

	// Import nodes into PebbleDB
	go func() {
		errCh <- m.stateStore.RawImport(ch)
	}()

	// Block until both processes complete
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	// Set earliest and latest version in the database
	err = m.stateStore.SetEarliestVersion(1)
	if err != nil {
		return err
	}

	return m.stateStore.SetLatestVersion(version)
}

func (m *Migrator) Verify(version int64) error {
	var verifyErr error
	for _, module := range modules {
		tree, err := ReadTree(m.iavlDB, version, []byte(buildTreePrefix(module)))
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

	for _, module := range modules {
		if !startModuleFound {
			if module == startModule {
				startModuleFound = true
			} else {
				continue
			}
		}
		startTimeModule := time.Now() // Measure time for each module
		fmt.Printf("SeiDB Archive Migration: Iterating through %s module...\n", module)

		prefixDB := dbm.NewPrefixDB(db, []byte(buildRawPrefix(module)))
		var itr dbm.Iterator
		var err error

		// If there is a starting key, seek to it, otherwise start from the beginning
		if startKey != nil && bytes.HasPrefix(startKey, []byte(buildRawPrefix(module))) {
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

func (m *Migrator) AggregateModuleStats(db dbm.DB) error {
	fmt.Println("SeiDB Archive Migration: Aggregating module stats...")

	for _, module := range modules {

		startTimeModule := time.Now()
		fmt.Printf("SeiDB Archive Migration: Aggregating stats for module %s...\n", module)

		// Prepare the prefixed DB for the module
		prefixDB := dbm.NewPrefixDB(db, []byte(buildRawPrefix(module)))

		// Get an iterator for the prefixed DB
		itr, err := prefixDB.Iterator(nil, nil)
		if err != nil {
			fmt.Printf("SeiDB Archive Migration: Error creating iterator: %+v\n", err)
			return fmt.Errorf("failed to create iterator: %w", err)
		}
		defer itr.Close()

		// Aggregated stats for the current module
		var totalKeys int
		var totalKeySize int
		var totalValueSize int

		for ; itr.Valid(); itr.Next() {
			value := bytes.Clone(itr.Value())

			node, err := iavl.MakeNode(value)
			if err != nil {
				return fmt.Errorf("failed to make node: %w", err)
			}

			// Only export leaf nodes
			if node.GetHeight() == 0 {
				totalKeys++
				totalKeySize += len(itr.Key())
				totalValueSize += len(itr.Value())
			}

			if totalKeys%1000000 == 0 {
				fmt.Printf("Module %s num of keys %d, key size %d, value size %d\n",
					module, totalKeys, totalKeySize, totalValueSize)
			}
		}

		if err := itr.Error(); err != nil {
			return fmt.Errorf("iterator error for module %s: %w", module, err)
		}

		moduleDuration := time.Since(startTimeModule)
		fmt.Printf("Module %s finished, total keys %d, total key size %d, total value size %d, time taken %s\n",
			module, totalKeys, totalKeySize, totalValueSize, moduleDuration)
	}

	fmt.Println("SeiDB Archive Migration: Aggregation completed.")
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
	_, err = tree.LoadVersion(version)
	return tree, err
}
