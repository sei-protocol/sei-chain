package ss

import (
	"bytes"
	"fmt"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/tools/utils"
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

func NewMigrator(db dbm.DB, stateStore types.StateStore) *Migrator {
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
	err = m.stateStore.SetEarliestVersion(1, true)
	if err != nil {
		return err
	}

	return m.stateStore.SetLatestVersion(version)
}

func (m *Migrator) MigrateAtHeight(migrationHeight int64, homeDir string) error {
	// Delete at migration height
	for _, module := range []string{
		"authz",
		"capability",
		"evm"} {
		fmt.Printf("Deleting keys for module %q at version %d...\n", module, migrationHeight)
		if err := m.stateStore.DeleteKeysAtVersion(module, migrationHeight); err != nil {
			return fmt.Errorf("failed to delete keys for module %q: %w", module, err)
		}
		fmt.Printf("Finished deletion for module %q\n", module)
	}

	rawCh := make(chan types.RawSnapshotNode, 1000)
	errCh := make(chan error, 2)

	// Re-export at migration height
	go func() {
		for _, module := range utils.Modules {
			if err := ExportLeafNodesAtVersion(m.iavlDB, migrationHeight, module, rawCh); err != nil {
				errCh <- fmt.Errorf("export error for module %q: %w", module, err)
				return
			}
		}
		close(rawCh)
		errCh <- nil
	}()

	// Launch a goroutine to import the exported nodes into PebbleDB using RawImport.
	go func() {
		errCh <- m.stateStore.RawImport(rawCh)
	}()

	// Wait for both export and import to complete.
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	fmt.Printf("Migration at height %d completed successfully\n", migrationHeight)

	ch := make(chan types.RawSnapshotNode, 1000)
	errCh = make(chan error, 2)

	// Re-migrate evm + capability
	go func() {
		defer close(ch)
		errCh <- ExportLeafNodesFromKey(m.iavlDB, ch, []byte{}, "")
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

	fmt.Printf("Migration for evm, capability and authz at height %d completed successfully\n", migrationHeight)

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

func ExportLeafNodesAtVersion(db dbm.DB, migrationVersion int64, module string, ch chan<- types.RawSnapshotNode) error {
	// Use the module-specific prefix.
	prefix := []byte(utils.BuildTreePrefix(module))
	tree, err := ReadTree(db, migrationVersion, prefix)
	if err != nil {
		return fmt.Errorf("failed to load IAVL tree for module %q at version %d: %w", module, migrationVersion, err)
	}

	stopped, err := tree.Iterate(func(key, value []byte) bool {
		node, err := iavl.MakeNode(value)
		if err != nil {
			fmt.Printf("failed to decode node for key %q in module %q: %v\n", key, module, err)
			return false // continue iteration
		}
		// Only export leaf nodes with height 0 and matching migrationVersion.
		if node.GetHeight() == 0 && node.GetVersion() == migrationVersion {
			ch <- types.RawSnapshotNode{
				StoreKey: module,
				Key:      node.GetNodeKey(),
				Value:    node.GetValue(),
				Version:  node.GetVersion(),
			}
		}
		return false
	})
	if stopped {
		return fmt.Errorf("iteration stopped unexpectedly")
	}
	if err != nil {
		return fmt.Errorf("error iterating IAVL tree for module %q: %w", module, err)
	}
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
