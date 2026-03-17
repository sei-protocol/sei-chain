package parquet

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	pqgo "github.com/parquet-go/parquet-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// createTestReceiptFile writes a valid receipt parquet file containing one
// receipt per block in [startBlock, startBlock+count), along with a matching
// log file. Returns an error instead of calling t.Fatal so it is safe to
// call from non-test goroutines.
func createTestReceiptFile(dir string, startBlock, count uint64) error {
	path := fmt.Sprintf("%s/receipts_%d.parquet", dir, startBlock)
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	type rec struct {
		TxHash       []byte `parquet:"tx_hash"`
		BlockNumber  uint64 `parquet:"block_number"`
		ReceiptBytes []byte `parquet:"receipt_bytes"`
	}
	w := pqgo.NewGenericWriter[rec](f)
	for i := uint64(0); i < count; i++ {
		block := startBlock + i
		txHash := common.BigToHash(new(big.Int).SetUint64(block))
		if _, err := w.Write([]rec{{
			TxHash:       txHash[:],
			BlockNumber:  block,
			ReceiptBytes: []byte{0x1},
		}}); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// Also write a matching log file so pruning finds the pair.
	logPath := fmt.Sprintf("%s/logs_%d.parquet", dir, startBlock)
	lf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	type logRec struct {
		BlockNumber uint64 `parquet:"block_number"`
	}
	lw := pqgo.NewGenericWriter[logRec](lf)
	if _, err := lw.Write([]logRec{{BlockNumber: startBlock}}); err != nil {
		return err
	}
	if err := lw.Close(); err != nil {
		return err
	}
	return lf.Close()
}

// TestConcurrentReadsAndPrune verifies that pruning waits for in-flight
// readers before deleting files. Without pruneMu this would race: a reader
// snapshots the file list, pruning deletes a file, and the DuckDB query
// fails with "No files found".
func TestConcurrentReadsAndPrune(t *testing.T) {
	dir := t.TempDir()

	// Create 3 receipt files spanning blocks 0-1499 (500 blocks each).
	for _, start := range []uint64{0, 500, 1000} {
		require.NoError(t, createTestReceiptFile(dir, start, 500))
	}

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

	g, _ := errgroup.WithContext(ctx)

	// Start readers that continuously query for receipts across all files.
	for i := 0; i < numReaders; i++ {
		g.Go(func() error {
			for j := 0; j < readsPerReader; j++ {
				// Query a tx hash in the middle file (block 750).
				txHash := common.BigToHash(new(big.Int).SetUint64(750))
				if _, err := store.GetReceiptByTxHash(ctx, txHash); err != nil {
					return fmt.Errorf("GetReceiptByTxHash(750): %w", err)
				}
				// Also query for a missing tx hash to exercise full file scan.
				if _, err := store.GetReceiptByTxHash(ctx, common.Hash{0xff}); err != nil {
					return fmt.Errorf("GetReceiptByTxHash(missing): %w", err)
				}
			}
			return nil
		})
	}

	// Concurrently prune files with startBlock < 600 (file 0).
	g.Go(func() error {
		store.PruneOldFiles(600)
		return nil
	})

	require.NoError(t, g.Wait())
}

// TestOnFileRotationNotBlockedByPruneMu verifies the structural property
// that OnFileRotation only acquires mu (the file-list lock), never pruneMu
// (the file-lifetime lock). We hold pruneMu.RLock to simulate in-flight
// readers; if OnFileRotation tried to acquire pruneMu it would deadlock.
func TestOnFileRotationNotBlockedByPruneMu(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, createTestReceiptFile(dir, 0, 1))

	store, err := NewStore(StoreConfig{
		DBDirectory:      dir,
		MaxBlocksPerFile: 500,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.Equal(t, 1, store.Reader.ClosedReceiptFileCount())

	// Simulate in-flight readers by holding pruneMu.RLock directly.
	store.Reader.pruneMu.RLock()
	defer store.Reader.pruneMu.RUnlock()

	// OnFileRotation only needs mu.Lock, so it must complete even while
	// pruneMu is held. If it touched pruneMu this would deadlock and the
	// test runner's timeout would catch it.
	store.Reader.OnFileRotation(500)

	require.Equal(t, 2, store.Reader.ClosedReceiptFileCount())
}

// TestConcurrentReadsPruneAndRotation exercises all three operations
// (reads, pruning, rotation) concurrently to verify no deadlocks or races.
// Unlike TestConcurrentReadsAndPrune which only tests reads vs pruning, this
// test adds file rotation (the commit path) to verify the three-way lock
// ordering between mu and pruneMu doesn't deadlock.
func TestConcurrentReadsPruneAndRotation(t *testing.T) {
	dir := t.TempDir()

	// Create 5 files (blocks 0-2499).
	for i := uint64(0); i < 5; i++ {
		require.NoError(t, createTestReceiptFile(dir, i*500, 500))
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
	var readErr atomic.Int64

	const numReaders = 10
	const readsPerReader = 50

	g, _ := errgroup.WithContext(ctx)

	// Readers: fixed number of queries.
	for i := 0; i < numReaders; i++ {
		g.Go(func() error {
			for j := 0; j < readsPerReader; j++ {
				if _, err := store.GetReceiptByTxHash(ctx, common.Hash{0xff}); err != nil {
					readErr.Add(1)
				}
			}
			return nil
		})
	}

	// Pruner: prune old files.
	g.Go(func() error {
		for i := 0; i < 3; i++ {
			store.PruneOldFiles(uint64(1600 + i*500))
		}
		return nil
	})

	// Rotator: create files on disk, then call OnFileRotation (mimics the
	// commit path which writes the file before notifying the reader).
	g.Go(func() error {
		for i := uint64(0); i < 5; i++ {
			startBlock := 5000 + i*500
			if err := createTestReceiptFile(dir, startBlock, 1); err != nil {
				return fmt.Errorf("createTestReceiptFile(%d): %w", startBlock, err)
			}
			store.Reader.OnFileRotation(startBlock)
		}
		return nil
	})

	require.NoError(t, g.Wait())
	assert.Equal(t, int64(0), readErr.Load(), "readers should not see errors during concurrent prune+rotation")
}
