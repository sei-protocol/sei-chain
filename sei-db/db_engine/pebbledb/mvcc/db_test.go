package mvcc

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sstest "github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

func TestStorageTestSuite(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	s := &sstest.StorageTestSuite{
		BaseStorageTestSuite: sstest.BaseStorageTestSuite{
			NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
				return OpenDB(dir, config)
			},
			Config:         pebbleConfig,
			EmptyBatchSize: 12,
		},
	}

	suite.Run(t, s)
}

// TestStorageTestSuiteDefaultComparer runs the base storage test suite with Pebble's DefaultComparer
// instead of MVCCComparer. This is useful for new databases that don't need backwards compatibility.
// Note: Iterator tests are not included because DefaultComparer doesn't have the Split function
// configured for MVCC key encoding, so NextPrefix/SeekLT operations won't work correctly.
// BaseStorageTestSuite contains only tests that work with both comparers.
func TestStorageTestSuiteDefaultComparer(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	pebbleConfig.UseDefaultComparer = true

	s := &sstest.BaseStorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
			return OpenDB(dir, config)
		},
		Config:         pebbleConfig,
		EmptyBatchSize: 12,
	}

	suite.Run(t, s)
}

func TestVersionedCheckpointPreservesFutureLiveMarker(t *testing.T) {
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend

	store, err := OpenDB(filepath.Join(t.TempDir(), "live"), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.SetLatestVersion(10))
	require.NoError(t, store.SetEarliestVersion(4, false))

	dest := filepath.Join(t.TempDir(), "snapshot")
	done := make(chan error, 1)
	types.ScheduleCheckpoint(store, dest, nil, func(err error) {
		done <- err
	})
	require.NoError(t, <-done)
	// The caller decides both markers. Neither has to match what the copy
	// inherited: the label comes from the barrier, and the earliest version is
	// reconciled across every tree in the snapshot.
	require.NoError(t, types.SetCheckpointMarkers(store, dest, 5, 5))

	require.Equal(t, int64(10), store.GetLatestVersion())
	require.Equal(t, int64(4), store.GetEarliestVersion())
	for key, want := range map[string]uint64{latestVersionKey: 10, earliestVersionKey: 4} {
		marker, closer, err := store.(*Database).storage.Get([]byte(key))
		require.NoError(t, err)
		require.Equal(t, want, binary.LittleEndian.Uint64(marker), "live %s changed", key)
		require.NoError(t, closer.Close())
	}

	checkpoint, err := OpenDB(dest, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, checkpoint.Close()) })
	require.Equal(t, int64(5), checkpoint.GetLatestVersion())
	require.Equal(t, int64(5), checkpoint.GetEarliestVersion())

	require.Error(t, types.SetCheckpointMarkers(store, dest, 5, 6),
		"an earliest version above the label describes an empty range")
}

func TestScheduledCheckpointCanBeCanceledAtBarrier(t *testing.T) {
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = config.PebbleDBBackend

	store, err := OpenDB(filepath.Join(t.TempDir(), "live"), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	dest := filepath.Join(t.TempDir(), "snapshot")
	done := make(chan error, 1)
	types.ScheduleCheckpoint(store, dest, func() bool { return false }, func(err error) {
		done <- err
	})

	require.ErrorIs(t, <-done, types.ErrCheckpointCanceled)
	require.NoDirExists(t, dest)
}
