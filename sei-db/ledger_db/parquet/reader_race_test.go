package parquet

import (
	"context"
	"math/big"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	pqgo "github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTestReceiptFile writes a valid receipt parquet file containing one
// receipt per block in [startBlock, startBlock+count).
func writeTestReceiptFile(t *testing.T, dir string, startBlock, count uint64) string {
	t.Helper()
	path := dir + "/" + "receipts_" + big.NewInt(int64(startBlock)).String() + ".parquet"
	f, err := os.Create(path)
	require.NoError(t, err)

	type rec struct {
		TxHash       []byte `parquet:"tx_hash"`
		BlockNumber  uint64 `parquet:"block_number"`
		ReceiptBytes []byte `parquet:"receipt_bytes"`
	}
	w := pqgo.NewGenericWriter[rec](f)
	for i := uint64(0); i < count; i++ {
		block := startBlock + i
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		_, err := w.Write([]rec{{
			TxHash:       txHash[:],
			BlockNumber:  block,
			ReceiptBytes: []byte{0x1},
		}})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())

	// Also write a matching log file so pruning finds the pair.
	logPath := dir + "/" + "logs_" + big.NewInt(int64(startBlock)).String() + ".parquet"
	lf, err := os.Create(logPath)
	require.NoError(t, err)
	type logRec struct {
		BlockNumber uint64 `parquet:"block_number"`
	}
	lw := pqgo.NewGenericWriter[logRec](lf)
	_, err = lw.Write([]logRec{{BlockNumber: startBlock}})
	require.NoError(t, err)
	require.NoError(t, lw.Close())
	require.NoError(t, lf.Close())

	return path
}

// TestConcurrentReadsAndPrune verifies that pruning waits for in-flight
// readers before deleting files. Without pruneMu this would race: a reader
// snapshots the file list, pruning deletes a file, and the DuckDB query
// fails with "No files found".
func TestConcurrentReadsAndPrune(t *testing.T) {
	dir := t.TempDir()

	// Create 3 receipt files spanning blocks 0-1499 (500 blocks each).
	writeTestReceiptFile(t, dir, 0, 500)
	writeTestReceiptFile(t, dir, 500, 500)
	writeTestReceiptFile(t, dir, 1000, 500)

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
		KeepRecent:       600,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Verify all 3 receipt files are tracked.
	require.Equal(t, 3, store.Reader.ClosedReceiptFileCount())

	ctx := context.Background()
	const numReaders = 20
	const readsPerReader = 50

	var wg sync.WaitGroup
	readErrors := make(chan error, numReaders*readsPerReader)

	// Start readers that continuously query for receipts across all files.
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				// Query a tx hash in the middle file (block 750).
				txHash := common.BigToHash(new(big.Int).SetUint64(750))
				_, err := store.GetReceiptByTxHash(ctx, txHash)
				if err != nil {
					readErrors <- err
				}
				// Also query for a missing tx hash to exercise full file scan.
				_, err = store.GetReceiptByTxHash(ctx, common.Hash{0xff})
				if err != nil {
					readErrors <- err
				}
			}
		}()
	}

	// Concurrently prune files with startBlock < 600 (file 0).
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.PruneOldFiles(600)
	}()

	wg.Wait()
	close(readErrors)

	for err := range readErrors {
		t.Errorf("reader got error during concurrent prune: %v", err)
	}
}

// TestOnFileRotationNotBlockedByReaders verifies that OnFileRotation (the
// commit hotpath) completes quickly even when many readers hold long-running
// queries. This is the core contention scenario from the issue report.
func TestOnFileRotationNotBlockedByReaders(t *testing.T) {
	dir := t.TempDir()

	// Create a large file so DuckDB queries take a bit longer.
	writeTestReceiptFile(t, dir, 0, 500)

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	const numReaders = 20

	var wg sync.WaitGroup
	readersStarted := make(chan struct{})

	// Start readers that hold pruneMu.RLock for the duration of their query.
	// They query for a missing hash to force a full scan.
	var readersReady sync.WaitGroup
	readersReady.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			readersReady.Done()
			<-readersStarted
			for j := 0; j < 100; j++ {
				_, _ = store.GetReceiptByTxHash(ctx, common.Hash{0xff})
			}
		}()
	}

	// Wait for all readers to be ready, then let them go.
	readersReady.Wait()
	close(readersStarted)

	// OnFileRotation should complete almost instantly because it only
	// touches mu, not pruneMu.
	start := time.Now()
	store.Reader.OnFileRotation(500)
	elapsed := time.Since(start)

	wg.Wait()

	// OnFileRotation should take well under 100ms. Before the fix it would
	// block until all readers finish (potentially hundreds of ms).
	assert.Less(t, elapsed, 100*time.Millisecond,
		"OnFileRotation took %v; should be near-instant since it doesn't touch pruneMu", elapsed)

	// Verify the file was actually added.
	require.Equal(t, 2, store.Reader.ClosedReceiptFileCount())
}

// TestConcurrentReadsPruneAndRotation exercises all three operations
// (reads, pruning, rotation) concurrently to verify no deadlocks or races.
func TestConcurrentReadsPruneAndRotation(t *testing.T) {
	dir := t.TempDir()

	// Create 5 files (blocks 0-2499).
	for i := uint64(0); i < 5; i++ {
		writeTestReceiptFile(t, dir, i*500, 500)
	}

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
		KeepRecent:       1000,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, 5, store.Reader.ClosedReceiptFileCount())

	ctx := context.Background()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var readErr atomic.Int64

	// Readers: continuously query.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, err := store.GetReceiptByTxHash(ctx, common.Hash{0xff})
				if err != nil {
					readErr.Add(1)
				}
			}
		}()
	}

	// Pruner: prune old files (only the original 5 files at blocks 0-2499).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			store.PruneOldFiles(uint64(1600 + i*500))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Rotator: create files on disk, then call OnFileRotation (mimics the
	// commit path which writes the file before notifying the reader).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(0); i < 5; i++ {
			startBlock := 5000 + i*500
			writeTestReceiptFile(t, dir, startBlock, 1)
			store.Reader.OnFileRotation(startBlock)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Let everything run for a bit.
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	assert.Equal(t, int64(0), readErr.Load(), "readers should not see errors during concurrent prune+rotation")
}
