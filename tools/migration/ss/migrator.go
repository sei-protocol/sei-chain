package ss

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/armon/go-metrics"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/tools/migration/utils"
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
	// Channel to send RawSnapshotNodes to the importer.
	ch := make(chan types.RawSnapshotNode, 1000)
	// Channel to capture errors from both goroutines below.
	errCh := make(chan error, 2)

	fmt.Printf("Starting migration for 'distribution' module from version %d to %d...\n", 100215000, 106789896)

	// Goroutine #1: Export distribution leaf nodes into ch
	go func() {
		defer close(ch)
		errCh <- exportDistributionLeafNodes(m.iavlDB, ch, 100215000, 106789896, 30)
	}()

	// Goroutine #2: Import those leaf nodes into PebbleDB
	go func() {
		errCh <- m.stateStore.RawImport(ch)
	}()

	// Wait for both goroutines to complete
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	return nil
}

func exportDistributionLeafNodes(
	db dbm.DB,
	ch chan<- types.RawSnapshotNode,
	startVersion, endVersion int64,
	concurrency int,
) error {
	fmt.Printf("Starting export at time: %s with concurrency=%d\n", time.Now().Format(time.RFC3339), concurrency)

	if concurrency < 1 {
		concurrency = 1
	}

	// We'll collect errors from each goroutine in a separate error channel
	errCh := make(chan error, concurrency)

	totalVersions := endVersion - startVersion + 1
	// Basic integer division to split up the version range
	chunkSize := totalVersions / int64(concurrency)
	remainder := totalVersions % int64(concurrency)

	var wg sync.WaitGroup
	wg.Add(concurrency)

	startTime := time.Now()
	// Atomic or shared counters for tracking exports
	var mu sync.Mutex
	totalExported := 0

	// Helper function that each goroutine will run
	workerFunc := func(workerID int, vStart, vEnd int64) {
		defer wg.Done()

		// Local counter
		localExportCount := 0

		for ver := vStart; ver <= vEnd; ver++ {
			// Load only the distribution module prefix at this version.
			tree, err := ReadTree(db, ver, []byte(utils.BuildTreePrefix("distribution")))
			if err != nil {
				errCh <- fmt.Errorf(
					"[worker %d] Error loading distribution tree at version %d: %w",
					workerID, ver, err,
				)
				return
			}

			var count int
			_, err = tree.Iterate(func(key, value []byte) bool {
				ch <- types.RawSnapshotNode{
					StoreKey: "distribution",
					Key:      key,
					Value:    value,
					Version:  ver,
				}
				count++
				// Use a lock when incrementing total counters
				mu.Lock()
				totalExported++
				mu.Unlock()

				// Logging / metrics every 1,000,000 keys in this version subset
				if count%1000000 == 0 {
					fmt.Printf("[worker %d][%s] Exported %d distribution keys at version %d so far\n",
						workerID, time.Now().Format(time.RFC3339), count, ver,
					)
					metrics.IncrCounterWithLabels(
						[]string{"sei", "migration", "leaf_nodes_exported"},
						float32(count),
						[]metrics.Label{
							{Name: "module", Value: "distribution"},
						},
					)
				}
				return false // continue iteration
			})
			if err != nil {
				errCh <- fmt.Errorf(
					"[worker %d] Error iterating distribution tree for version %d: %w",
					workerID, ver, err,
				)
				return
			}

			localExportCount += count

			fmt.Printf("[worker %d][%s] Finished versions %d: exported %d distribution keys.\n",
				workerID, time.Now().Format(time.RFC3339), ver, localExportCount)
		}

		fmt.Printf("[worker %d][%s]  Finished versions [%d - %d]: exported %d distribution keys.\n",
			workerID, time.Now().Format(time.RFC3339), vStart, vEnd, localExportCount)
		// Signal that we're done successfully (no error).
		errCh <- nil
	}

	// Spawn the workers
	var currentVersion int64 = startVersion
	for i := 0; i < concurrency; i++ {
		// Each goroutine gets a range: [currentVersion, currentVersion+chunkSize-1]
		// plus we handle any remainder in the last chunk(s).
		extra := int64(0)
		if i < int(remainder) {
			extra = 1
		}
		workerStart := currentVersion
		workerEnd := currentVersion + chunkSize + extra - 1
		if i == concurrency-1 {
			// Make sure the last one includes everything up to endVersion
			workerEnd = endVersion
		}
		currentVersion = workerEnd + 1

		go workerFunc(i, workerStart, workerEnd)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Check error channel. If any goroutine returned a non-nil error, return that.
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	fmt.Printf(
		"[%s] Completed exporting distribution module from %d to %d. Total keys: %d. Duration: %s\n",
		time.Now().Format(time.RFC3339), startVersion, endVersion, totalExported, time.Since(startTime),
	)
	fmt.Printf("Finished export at time: %s\n", time.Now().Format(time.RFC3339))
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
