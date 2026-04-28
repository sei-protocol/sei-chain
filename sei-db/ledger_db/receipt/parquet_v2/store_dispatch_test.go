package parquet_v2

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func newDispatchStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      t.TempDir(),
		MaxBlocksPerFile: 4,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestUnimplementedOperationsDispatchThroughCoordinator(t *testing.T) {
	ctx := context.Background()
	txHash := common.HexToHash("0x1")

	tests := []struct {
		name string
		run  func(*Store) error
	}{
		{
			name: "write receipts",
			run: func(store *Store) error {
				return store.WriteReceipts(nil)
			},
		},
		{
			name: "get receipt by tx hash",
			run: func(store *Store) error {
				_, err := store.GetReceiptByTxHash(ctx, txHash)
				return err
			},
		},
		{
			name: "get receipt by tx hash in block",
			run: func(store *Store) error {
				_, err := store.GetReceiptByTxHashInBlock(ctx, txHash, 1)
				return err
			},
		},
		{
			name: "get logs",
			run: func(store *Store) error {
				_, err := store.GetLogs(ctx, parquet.LogFilter{})
				return err
			},
		},
		{
			name: "observe empty block",
			run: func(store *Store) error {
				return store.ObserveEmptyBlock(1)
			},
		},
		{
			name: "replay WAL",
			run: func(store *Store) error {
				_, err := store.ReplayWAL(nil)
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newDispatchStore(t)
			require.ErrorIs(t, tc.run(store), ErrNotImplemented)
		})
	}
}

func TestMetadataAndConfigRequestsDispatchThroughCoordinator(t *testing.T) {
	store := newDispatchStore(t)
	require.Zero(t, cap(store.requests))

	require.Equal(t, uint64(0), store.FileStartBlock())
	require.Equal(t, int64(0), store.LatestVersion())
	require.Equal(t, uint64(4), store.CacheRotateInterval())
	require.True(t, store.IsRotationBoundary(8))
	require.False(t, store.IsRotationBoundary(9))

	store.SetLatestVersion(10)
	require.Equal(t, int64(10), store.LatestVersion())

	store.UpdateLatestVersion(8)
	require.Equal(t, int64(10), store.LatestVersion())

	store.UpdateLatestVersion(12)
	require.Equal(t, int64(12), store.LatestVersion())

	store.SetEarliestVersion(3)
	store.SetBlockFlushInterval(2)
	store.SetFaultHooks(&parquet.FaultHooks{})

	store.SetMaxBlocksPerFile(3)
	require.Equal(t, uint64(3), store.CacheRotateInterval())
	require.True(t, store.IsRotationBoundary(6))
	require.False(t, store.IsRotationBoundary(8))
}

func TestSetMaxBlocksPerFileUpdatesReaderState(t *testing.T) {
	reader, err := NewReaderWithMaxBlocksPerFile(t.TempDir(), 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	resp := make(chan error, 1)
	coord := &coordinator{
		config: parquet.StoreConfig{
			MaxBlocksPerFile: 10,
		},
		reader: reader,
	}

	coord.handleSetMaxBlocksPerFile(setMaxBlocksPerFileReq{
		maxBlocksPerFile: 3,
		resp:             resp,
	})

	require.NoError(t, <-resp)
	require.Equal(t, uint64(3), coord.config.MaxBlocksPerFile)
	require.Equal(t, uint64(3), reader.maxBlocksPerFile)
}

func TestUnbufferedRequestsApplyBackpressure(t *testing.T) {
	requests := make(chan coordRequest)
	done := make(chan struct{})
	coord := &coordinator{
		requests: requests,
		done:     done,
	}
	store := &Store{
		requests: requests,
		done:     done,
	}
	go coord.run()

	require.Zero(t, cap(store.requests))

	firstResp := make(chan writeResp)
	store.requests <- writeReq{resp: firstResp}
	time.Sleep(10 * time.Millisecond)

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- store.Flush()
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second request completed before first unblocked: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	require.ErrorIs(t, (<-firstResp).err, ErrNotImplemented)
	require.NoError(t, <-secondDone)
	require.NoError(t, store.Close())
}

func TestCloseStopsFutureRequests(t *testing.T) {
	store, err := NewStore(parquet.StoreConfig{DBDirectory: t.TempDir()})
	require.NoError(t, err)

	require.NoError(t, store.Close())
	require.ErrorIs(t, store.WriteReceipts(nil), ErrStoreClosed)
	require.NoError(t, store.Close())
}

func TestSimulateCrashStopsFutureRequests(t *testing.T) {
	store, err := NewStore(parquet.StoreConfig{DBDirectory: t.TempDir()})
	require.NoError(t, err)

	store.SimulateCrash()
	require.ErrorIs(t, store.WriteReceipts(nil), ErrStoreClosed)
	require.NoError(t, store.Close())
}
