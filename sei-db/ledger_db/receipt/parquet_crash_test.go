package receipt

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	dbLogger "github.com/sei-protocol/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errSimulatedCrash = errors.New("simulated crash")

func extractParquetStore(t *testing.T, store ReceiptStore) *parquet.Store {
	t.Helper()
	cached, ok := store.(*cachedReceiptStore)
	require.True(t, ok, "expected *cachedReceiptStore")
	pq, ok := cached.backend.(*parquetReceiptStore)
	require.True(t, ok, "expected *parquetReceiptStore backend")
	return pq.store
}

// TestCrashRecoveryAtEachHookPoint crashes the store once at each of the five
// fault-injection points in the write pipeline (AfterWALWrite, BeforeFlush,
// AfterFlush, AfterCloseWriters, AfterWALClear), then reopens it and verifies
// that pre-crash blocks, the crash block itself, and post-recovery writes are
// all readable. Rotation hooks (AfterCloseWriters, AfterWALClear) require 500
// pre-crash blocks to trigger a file rotation on the crash block.
func TestCrashRecoveryAtEachHookPoint(t *testing.T) {
	type hookSetup struct {
		name          string
		needsRotation bool
		install       func(h *parquet.FaultHooks, trigger func() error)
	}

	hookPoints := []hookSetup{
		{
			name: "AfterWALWrite",
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterWALWrite = func(_ uint64) error { return trigger() }
			},
		},
		{
			name: "BeforeFlush",
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.BeforeFlush = func(_ uint64) error { return trigger() }
			},
		},
		{
			name: "AfterFlush",
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterFlush = func(_ uint64) error { return trigger() }
			},
		},
		{
			name:          "AfterCloseWriters",
			needsRotation: true,
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterCloseWriters = func(_ uint64) error { return trigger() }
			},
		},
		{
			name:          "AfterWALClear",
			needsRotation: true,
			install: func(h *parquet.FaultHooks, trigger func() error) {
				h.AfterWALClear = func(_ uint64) error { return trigger() }
			},
		},
	}

	for _, hp := range hookPoints {
		t.Run(hp.name, func(t *testing.T) {
			ctx, storeKey := newTestContext()
			cfg := dbconfig.DefaultReceiptStoreConfig()
			cfg.Backend = "parquet"
			cfg.DBDirectory = t.TempDir()

			store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
			require.NoError(t, err)

			pqStore := extractParquetStore(t, store)

			// Rotation hooks (AfterCloseWriters, AfterWALClear) only fire when
			// blocksInFile >= MaxBlocksPerFile (default 500). We write exactly
			// 500 pre-crash blocks so that the crash block (501) is the one
			// that triggers rotation. Non-rotation hooks fire on every write,
			// so a handful of pre-crash blocks suffices.
			preBlocks := uint64(5)
			if hp.needsRotation {
				preBlocks = 500
			}

			addr := common.HexToAddress("0x1")

			// Write pre-crash blocks cleanly.
			for block := uint64(1); block <= preBlocks; block++ {
				txHash := blockTxHash(block)
				receipt := makeTestReceipt(txHash, block, 0, addr, nil)
				require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
					{TxHash: txHash, Receipt: receipt},
				}), "pre-crash block %d", block)
			}

			// Arm the fault hook so the next write crashes.
			fired := false
			hooks := &parquet.FaultHooks{}
			hp.install(hooks, func() error {
				if !fired {
					fired = true
					return errSimulatedCrash
				}
				return nil
			})
			pqStore.FaultHooks = hooks

			// Write the crashing block. The WAL entry is written before any
			// hook fires, so the crashing block should be recoverable.
			crashBlock := preBlocks + 1
			crashTxHash := blockTxHash(crashBlock)
			crashReceipt := makeTestReceipt(crashTxHash, crashBlock, 0, addr, nil)
			err = store.SetReceipts(ctx.WithBlockHeight(int64(crashBlock)), []ReceiptRecord{
				{TxHash: crashTxHash, Receipt: crashReceipt},
			})
			require.ErrorIs(t, err, errSimulatedCrash)

			pqStore.SimulateCrash()

			// --- Reopen and verify recovery ---

			store, err = NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			// Verify all pre-crash blocks are recovered.
			for block := uint64(1); block <= preBlocks; block++ {
				txHash := blockTxHash(block)
				got, err := store.GetReceiptFromStore(ctx, txHash)
				require.NoError(t, err, "pre-crash block %d not recovered", block)
				require.Equal(t, txHash.Hex(), got.TxHashHex)
			}

			// The crashing block must also be recovered (it was WAL-committed).
			// Exception: AfterWALClear truncates the WAL and the block is
			// already persisted in the closed parquet file, so it's still
			// recoverable via the parquet reader rather than WAL replay.
			got, err := store.GetReceiptFromStore(ctx, crashTxHash)
			require.NoError(t, err, "crash block %d not recovered", crashBlock)
			require.Equal(t, crashTxHash.Hex(), got.TxHashHex)

			// Verify the store is healthy: write and read a new block.
			postBlock := crashBlock + 1
			postTxHash := blockTxHash(postBlock)
			postReceipt := makeTestReceipt(postTxHash, postBlock, 0, addr, nil)
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(postBlock)), []ReceiptRecord{
				{TxHash: postTxHash, Receipt: postReceipt},
			}))
			got, err = store.GetReceiptFromStore(ctx, postTxHash)
			require.NoError(t, err, "post-recovery block %d should be readable", postBlock)
			require.Equal(t, postTxHash.Hex(), got.TxHashHex)
		})
	}
}

// nonRotationHooks returns the three hook installers that fire on every write
// (i.e. they don't require a file rotation to trigger).
func nonRotationHooks() []struct {
	name    string
	install func(h *parquet.FaultHooks, trigger func() error)
} {
	return []struct {
		name    string
		install func(h *parquet.FaultHooks, trigger func() error)
	}{
		{"AfterWALWrite", func(h *parquet.FaultHooks, trigger func() error) {
			h.AfterWALWrite = func(_ uint64) error { return trigger() }
		}},
		{"BeforeFlush", func(h *parquet.FaultHooks, trigger func() error) {
			h.BeforeFlush = func(_ uint64) error { return trigger() }
		}},
		{"AfterFlush", func(h *parquet.FaultHooks, trigger func() error) {
			h.AfterFlush = func(_ uint64) error { return trigger() }
		}},
	}
}

// TestCrashRecoveryStress runs 30 randomized trials, each with 1-3 chained
// crash-recover cycles. Each cycle writes a random number of blocks, crashes
// at a randomly chosen non-rotation hook point, then reopens the store. After
// all cycles complete, every block ever written (including crash blocks) must
// be recoverable via WAL replay.
func TestCrashRecoveryStress(t *testing.T) {
	seed := int64(42)
	t.Logf("random seed: %d (change to reproduce a specific run)", seed)
	rng := rand.New(rand.NewSource(seed))

	hooks := nonRotationHooks()
	const numTrials = 30

	for trial := 0; trial < numTrials; trial++ {
		numCrashes := 1 + rng.Intn(3) // 1-3 crash-recover cycles per trial
		t.Run(fmt.Sprintf("trial_%02d_%d_crashes", trial, numCrashes), func(t *testing.T) {
			ctx, storeKey := newTestContext()
			cfg := dbconfig.DefaultReceiptStoreConfig()
			cfg.Backend = "parquet"
			cfg.DBDirectory = t.TempDir()

			addr := common.HexToAddress("0x1")
			nextBlock := uint64(1)

			for crash := 0; crash < numCrashes; crash++ {
				store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
				require.NoError(t, err)

				pqStore := extractParquetStore(t, store)

				preBlocks := 1 + uint64(rng.Intn(15))
				for i := uint64(0); i < preBlocks; i++ {
					block := nextBlock
					nextBlock++
					txHash := blockTxHash(block)
					receipt := makeTestReceipt(txHash, block, 0, addr, nil)
					require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
						{TxHash: txHash, Receipt: receipt},
					}))
				}

				hook := hooks[rng.Intn(len(hooks))]
				fired := false
				fh := &parquet.FaultHooks{}
				hook.install(fh, func() error {
					if !fired {
						fired = true
						return errSimulatedCrash
					}
					return nil
				})
				pqStore.FaultHooks = fh

				crashBlock := nextBlock
				nextBlock++
				crashTxHash := blockTxHash(crashBlock)
				crashReceipt := makeTestReceipt(crashTxHash, crashBlock, 0, addr, nil)
				err = store.SetReceipts(ctx.WithBlockHeight(int64(crashBlock)), []ReceiptRecord{
					{TxHash: crashTxHash, Receipt: crashReceipt},
				})
				require.ErrorIs(t, err, errSimulatedCrash,
					"crash %d: hook %s on block %d", crash, hook.name, crashBlock)

				pqStore.SimulateCrash()
			}

			// Final reopen: every block (including crash blocks) should be
			// recoverable because the WAL entry is always written first.
			store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			lastBlock := nextBlock - 1
			for block := uint64(1); block <= lastBlock; block++ {
				txHash := blockTxHash(block)
				got, err := store.GetReceiptFromStore(ctx, txHash)
				require.NoError(t, err, "block %d not recovered", block)
				require.Equal(t, txHash.Hex(), got.TxHashHex)
			}

			// Confirm post-recovery writes work.
			postBlock := nextBlock
			postTxHash := blockTxHash(postBlock)
			postReceipt := makeTestReceipt(postTxHash, postBlock, 0, addr, nil)
			require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(int64(postBlock)), []ReceiptRecord{
				{TxHash: postTxHash, Receipt: postReceipt},
			}))
			got, err := store.GetReceiptFromStore(ctx, postTxHash)
			require.NoError(t, err)
			require.Equal(t, postTxHash.Hex(), got.TxHashHex)
		})
	}
}

// TestSlowFlushWithConcurrentReads verifies that concurrent readers see
// consistent data while writes are slow. A writer goroutine writes 50 blocks
// with random 5-50ms sleeps injected at each non-rotation hook point
// (simulating slow I/O). A reader goroutine continuously picks a random
// committed block and calls GetReceiptFromStore and FilterLogs, asserting no
// errors and correct data. After the writer finishes, a final sweep confirms
// every block is readable.
func TestSlowFlushWithConcurrentReads(t *testing.T) {
	ctx, storeKey := newTestContext()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "parquet"
	cfg.DBDirectory = t.TempDir()

	store, err := NewReceiptStore(dbLogger.NewNopLogger(), cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	pqStore := extractParquetStore(t, store)

	rng := rand.New(rand.NewSource(99))
	pqStore.FaultHooks = &parquet.FaultHooks{
		AfterWALWrite: func(_ uint64) error {
			time.Sleep(time.Duration(5+rng.Intn(46)) * time.Millisecond)
			return nil
		},
		BeforeFlush: func(_ uint64) error {
			time.Sleep(time.Duration(5+rng.Intn(46)) * time.Millisecond)
			return nil
		},
		AfterFlush: func(_ uint64) error {
			time.Sleep(time.Duration(5+rng.Intn(46)) * time.Millisecond)
			return nil
		},
	}

	const totalBlocks = 50
	addr := common.HexToAddress("0x1")
	topic := common.HexToHash("0xbeef")

	// Tracks the highest block that has been fully committed.
	var committed atomic.Uint64

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Writer goroutine: write blocks sequentially with slow hooks.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for block := uint64(1); block <= totalBlocks; block++ {
			txHash := blockTxHash(block)
			receipt := makeTestReceipt(txHash, block, 0, addr, []common.Hash{topic})
			if err := store.SetReceipts(ctx.WithBlockHeight(int64(block)), []ReceiptRecord{
				{TxHash: txHash, Receipt: receipt},
			}); err != nil {
				t.Errorf("writer: SetReceipts block %d: %v", block, err)
				return
			}
			committed.Store(block)
		}
	}()

	// Reader goroutine: continuously read committed blocks.
	wg.Add(1)
	go func() {
		defer wg.Done()
		readerRng := rand.New(rand.NewSource(7))
		for {
			select {
			case <-done:
				return
			default:
			}

			hi := committed.Load()
			if hi == 0 {
				time.Sleep(time.Millisecond)
				continue
			}
			block := 1 + uint64(readerRng.Int63n(int64(hi)))
			txHash := blockTxHash(block)

			got, err := store.GetReceiptFromStore(ctx, txHash)
			if !assert.NoError(t, err, "reader: GetReceiptFromStore block %d", block) {
				continue
			}
			assert.Equal(t, txHash.Hex(), got.TxHashHex,
				"reader: wrong receipt for block %d", block)

			logs, err := store.FilterLogs(ctx, block, block, filters.FilterCriteria{
				Addresses: []common.Address{addr},
				Topics:    [][]common.Hash{{topic}},
			})
			if !assert.NoError(t, err, "reader: FilterLogs block %d", block) {
				continue
			}
			for _, lg := range logs {
				assert.Equal(t, block, lg.BlockNumber,
					"reader: log block mismatch for query block %d", block)
			}
		}
	}()

	wg.Wait()

	// Final consistency check: every block is readable.
	for block := uint64(1); block <= totalBlocks; block++ {
		txHash := blockTxHash(block)
		got, err := store.GetReceiptFromStore(ctx, txHash)
		require.NoError(t, err, "final: block %d", block)
		require.Equal(t, txHash.Hex(), got.TxHashHex)
	}
}

// blockTxHash returns a deterministic tx hash for a block number.
func blockTxHash(block uint64) common.Hash {
	return common.BigToHash(new(big.Int).SetUint64(block))
}
