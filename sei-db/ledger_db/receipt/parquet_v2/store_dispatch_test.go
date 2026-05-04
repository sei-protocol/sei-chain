package parquet_v2

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/stretchr/testify/require"
)

func newDispatchStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(parquet.StoreConfig{
		DBDirectory:      t.TempDir(),
		MaxBlocksPerFile: 4,
	}, ReplayHooks{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMetadataAndConfigRequestsDispatchThroughCoordinator(t *testing.T) {
	store := newDispatchStore(t)

	require.Equal(t, uint64(0), store.FileStartBlock())
	require.Equal(t, int64(0), store.LatestVersion())
	require.Equal(t, uint64(4), store.CacheRotateInterval())

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
}

func TestCloseStopsFutureRequests(t *testing.T) {
	store, err := NewStore(parquet.StoreConfig{DBDirectory: t.TempDir()}, ReplayHooks{})
	require.NoError(t, err)

	require.NoError(t, store.Close())
	require.ErrorIs(t, store.WriteReceipts(0, nil), ErrStoreClosed)
	require.NoError(t, store.Close())
}

func TestSimulateCrashStopsFutureRequests(t *testing.T) {
	store, err := NewStore(parquet.StoreConfig{DBDirectory: t.TempDir()}, ReplayHooks{})
	require.NoError(t, err)

	store.SimulateCrash()
	require.ErrorIs(t, store.WriteReceipts(0, nil), ErrStoreClosed)
	require.NoError(t, store.Close())
}
