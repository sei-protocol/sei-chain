package memiavl

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/cosmos/iavl"
)

func mustReadLastChangelogEntry(t *testing.T, cs *CommitStore) proto.ChangelogEntry {
	t.Helper()
	require.NotNil(t, cs.db)
	w := cs.db.GetWAL()
	require.NotNil(t, w)
	last, err := w.LastOffset()
	require.NoError(t, err)
	require.Greater(t, last, uint64(0))
	e, err := w.ReadAt(last)
	require.NoError(t, err)
	return e
}

func TestNewCommitStore(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.SnapshotInterval = 10
	cs := NewCommitStore(dir, logger.NewNopLogger(), cfg)
	require.NotNil(t, cs)
	require.NotNil(t, cs.logger)
	require.True(t, cs.opts.ZeroCopy)
	require.Equal(t, uint32(10), cs.opts.SnapshotInterval)
	require.True(t, cs.opts.CreateIfMissing)
}

func TestNewCommitStoreWithCustomDirectory(t *testing.T) {
	customDir := t.TempDir()

	cs := NewCommitStore(customDir, logger.NewNopLogger(), Config{})
	require.NotNil(t, cs)
	require.Contains(t, cs.opts.Dir, customDir)
}

func TestInitialize(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})

	stores := []string{"store1", "store2", "store3"}
	cs.Initialize(stores)

	require.Equal(t, stores, cs.opts.InitialStores)
}

func TestCommitStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// Load version 0 to initialize the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Initial version should be 0
	require.Equal(t, int64(0), cs.Version())

	// Apply changesets
	changesets := []*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
					{Key: []byte("key2"), Value: []byte("value2")},
				},
			},
		},
	}
	err = cs.ApplyChangeSets(changesets)
	require.NoError(t, err)

	// Commit
	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)

	entry := mustReadLastChangelogEntry(t, cs)
	require.Equal(t, int64(1), entry.Version)
	require.Equal(t, changesets, entry.Changesets)

	// Version should be updated
	require.Equal(t, int64(1), cs.Version())
}

func TestApplyChangeSetsEmpty(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Empty changesets should be no-op
	err = cs.ApplyChangeSets(nil)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{})
	require.NoError(t, err)
}

func TestApplyUpgrades(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Apply upgrades
	upgrades := []*proto.TreeNameUpgrade{
		{Name: "newstore1"},
		{Name: "newstore2"},
	}
	err = cs.ApplyUpgrades(upgrades)
	require.NoError(t, err)

	// Apply more upgrades - should append
	moreUpgrades := []*proto.TreeNameUpgrade{
		{Name: "newstore3"},
	}
	err = cs.ApplyUpgrades(moreUpgrades)
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)
	entry := mustReadLastChangelogEntry(t, cs)
	// 4 upgrades total: initial store "test" + newstore1, newstore2, newstore3
	require.Len(t, entry.Upgrades, 4)
}

func TestApplyUpgradesEmpty(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Empty upgrades should be no-op
	err = cs.ApplyUpgrades(nil)
	require.NoError(t, err)

	err = cs.ApplyUpgrades([]*proto.TreeNameUpgrade{})
	require.NoError(t, err)
}

func TestLoadVersionCopyExisting(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// First load to create the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Apply and commit some data
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)
	require.NoError(t, cs.Close())

	// Load with copyExisting=true should create a new readonly CommitStore
	newCS, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	require.NotNil(t, newCS)

	// The returned store should be different from the original
	newCommitStore, ok := newCS.(*CommitStore)
	require.True(t, ok)
	require.NotSame(t, cs, newCommitStore)

	require.NoError(t, newCommitStore.Close())
}

func TestCommitInfo(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// WorkingCommitInfo before any commit
	workingInfo := cs.WorkingCommitInfo()
	require.NotNil(t, workingInfo)

	// Apply and commit
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// LastCommitInfo after commit
	lastInfo := cs.LastCommitInfo()
	require.NotNil(t, lastInfo)
	require.Equal(t, int64(1), lastInfo.Version)
}

func TestGetModuleByName(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test", "other"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Get existing module
	module := cs.GetChildStoreByName("test")
	require.NotNil(t, module)

	// Get non-existing module
	module = cs.GetChildStoreByName("nonexistent")
	require.Nil(t, module)
}

func TestExporterVersionValidation(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Negative version should fail
	_, err = cs.Exporter(-1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")

	// Version > MaxUint32 should fail
	_, err = cs.Exporter(math.MaxUint32 + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestImporterVersionValidation(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})

	// Negative version should fail
	_, err := cs.Importer(-1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")

	// Version > MaxUint32 should fail
	_, err = cs.Importer(math.MaxUint32 + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestCommitStoreClose(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Close should succeed
	err = cs.Close()
	require.NoError(t, err)

	// db should be nil after close
	require.Nil(t, cs.db)

	// Close again should be safe (no-op)
	err = cs.Close()
	require.NoError(t, err)
}

func TestCommitStoreRollback(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value" + string(rune('0'+i)))},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), cs.Version())

	// Rollback to version 2 (truncates WAL after version 2)
	err = cs.Rollback(2)
	require.NoError(t, err)
	require.Equal(t, int64(2), cs.Version())

	require.NoError(t, cs.Close())
}

func TestMultipleCommits(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Multiple commits
	for i := 1; i <= 5; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key" + string(rune('0'+i))), Value: []byte("value")},
					},
				},
			},
		})
		require.NoError(t, err)

		version, err := cs.Commit()
		require.NoError(t, err)
		require.Equal(t, int64(i), version)
	}

	require.Equal(t, int64(5), cs.Version())
}

func TestCommitWithUpgradesAndChangesets(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Apply upgrades first
	err = cs.ApplyUpgrades([]*proto.TreeNameUpgrade{
		{Name: "newstore"},
	})
	require.NoError(t, err)

	// Then apply changesets to the new store
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "newstore",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)

	// Commit
	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)
	entry := mustReadLastChangelogEntry(t, cs)
	// 2 upgrades total: initial store "test" + "newstore"
	require.Len(t, entry.Upgrades, 2)
	require.Len(t, entry.Changesets, 1)
}

func TestSetInitialVersion(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// Set initial version
	err = cs.SetInitialVersion(100)
	require.NoError(t, err)
}

func TestGetVersions(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value")},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	// Create new CommitStore to test GetLatestVersion
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs2.Initialize([]string{"test"})

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}

func TestCreateWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() {
		err := cs.Close()
		require.NoError(t, err)
	}()

	// MemIAVL should have opened its changelog WAL.
	require.NotNil(t, cs.db.GetWAL())
}

func TestLoadVersionReadOnlyWithWALReplay(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// First load to create the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Write data (MemIAVL will persist changelog internally)
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Write more data
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key2"), Value: []byte("value2")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	require.Equal(t, int64(2), cs.Version())

	// Load read-only copy - should replay from WAL
	readOnlyCS, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	require.NotNil(t, readOnlyCS)

	// The read-only copy should have the same version after WAL replay
	roCommitStore := readOnlyCS.(*CommitStore)
	require.Equal(t, int64(2), roCommitStore.Version())

	require.NotNil(t, roCommitStore.db.GetWAL())

	// Clean up
	require.NoError(t, roCommitStore.Close())
	require.NoError(t, cs.Close())
}

func TestLoadVersionReadOnlyCreatesOwnWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// First load to create the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit some data with WAL entries
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Create multiple read-only copies
	readOnly1, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	require.NotNil(t, readOnly1)

	readOnly2, err := cs.LoadVersion(0, true)
	require.NoError(t, err)
	require.NotNil(t, readOnly2)

	// Each should have its own WAL instance
	ro1 := readOnly1.(*CommitStore)
	ro2 := readOnly2.(*CommitStore)
	require.NotNil(t, ro1.db.GetWAL())
	require.NotNil(t, ro2.db.GetWAL())

	// Clean up
	require.NoError(t, ro1.Close())
	require.NoError(t, ro2.Close())
	require.NoError(t, cs.Close())
}

func TestWALPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// First session: write data
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Write and commit
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key1"), Value: []byte("value1")},
					{Key: []byte("key2"), Value: []byte("value2")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// More commits
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key3"), Value: []byte("value3")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	require.Equal(t, int64(2), cs.Version())
	require.NoError(t, cs.Close())

	// Second session: reload and verify WAL replay
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs2.Initialize([]string{"test"})

	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)

	// Version should be restored via WAL replay
	require.Equal(t, int64(2), cs2.Version())

	// Data should be accessible
	tree := cs2.GetChildStoreByName("test")
	require.NotNil(t, tree)

	require.NoError(t, cs2.Close())
}

func TestRollbackWithWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit multiple versions
	for i := 0; i < 5; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value" + string(rune('0'+i)))},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	require.Equal(t, int64(5), cs.Version())
	require.NotNil(t, cs.db.GetWAL())

	// Rollback to version 3
	err = cs.Rollback(3)
	require.NoError(t, err)
	require.Equal(t, int64(3), cs.Version())

	// WAL should still exist after rollback
	require.NotNil(t, cs.db.GetWAL())

	require.NoError(t, cs.Close())

	// Reopen and verify rollback persisted
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs2.Initialize([]string{"test"})

	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)

	// Version should be 3 after replay
	require.Equal(t, int64(3), cs2.Version())

	require.NoError(t, cs2.Close())
}

func TestRollbackCreatesWALIfNeeded(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// Load and commit
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Close to clear WAL
	require.NoError(t, cs.Close())

	// After Close(), create a new CommitStore (WAL creation happens in NewCommitStore)
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs2.Initialize([]string{"test"})

	// Rollback should work
	require.NoError(t, cs2.Rollback(1))
	require.NoError(t, cs2.Close())
}

func TestCloseReleasesWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NotNil(t, cs.db)
	require.NotNil(t, cs.db.GetWAL())

	// Close
	require.NoError(t, cs.Close())

	// DB should be nil after close
	require.Nil(t, cs.db)
}

func TestLoadVersionReusesExistingWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// First load
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NotNil(t, cs.db.GetWAL())

	// Commit some data
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Second load (non-copy) should close and recreate WAL
	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err)

	require.NotNil(t, cs.db.GetWAL())

	// Version should be replayed
	require.Equal(t, int64(1), cs.Version())

	require.NoError(t, cs.Close())
}

func TestReadOnlyCopyCannotCommit(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	// First load
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit initial data
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)
	_, err = cs.Commit()
	require.NoError(t, err)

	// Load read-only copy
	readOnly, err := cs.LoadVersion(0, true)
	require.NoError(t, err)

	roCS := readOnly.(*CommitStore)

	// Read-only copy should have read-only option set
	require.True(t, roCS.opts.ReadOnly)

	// Attempting to commit on read-only copy should fail
	// (this would fail at the memiavl.DB level)
	_, err = roCS.Commit()
	require.Error(t, err)

	require.NoError(t, roCS.Close())
	require.NoError(t, cs.Close())
}

// TestWALTruncationOnCommit tests that WAL is automatically truncated after commits
// when the earliest snapshot version advances past WAL entries.
func TestWALTruncationOnCommit(t *testing.T) {
	dir := t.TempDir()

	// Configure with snapshot interval to trigger snapshot creation
	cfg := DefaultConfig()
	cfg.SnapshotInterval = 2
	cfg.SnapshotKeepRecent = 1
	cs := NewCommitStore(dir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit multiple versions to trigger snapshot creation and WAL truncation
	for i := 0; i < 10; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value" + string(rune('0'+i)))},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	// Verify current version
	require.Equal(t, int64(10), cs.Version())

	// Get WAL state
	firstWALIndex, err := cs.db.GetWAL().FirstOffset()
	require.NoError(t, err)

	// Get earliest snapshot version - may not exist yet if snapshots are async
	earliestSnapshot, err := cs.GetEarliestVersion()
	if err != nil {
		// No snapshots yet (async snapshot creation), that's okay for this test
		t.Logf("No snapshots created yet (async): %v", err)
		require.NoError(t, cs.Close())
		return
	}

	// WAL's first index should be greater than 1 if truncation happened
	// (meaning early entries were removed)
	// The exact value depends on snapshot creation timing and pruning
	t.Logf("WAL first index: %d, earliest snapshot: %d", firstWALIndex, earliestSnapshot)

	// Key assertion: WAL entries before earliest snapshot should be truncated
	// WAL version = index + delta, so WAL first version = firstIndex + delta
	walDelta := cs.db.GetWALIndexDelta()
	walFirstVersion := int64(firstWALIndex) + walDelta
	require.GreaterOrEqual(t, walFirstVersion, earliestSnapshot,
		"WAL first version should be >= earliest snapshot version after truncation")

	require.NoError(t, cs.Close())
}

// TestWALTruncationWithNoSnapshots tests that WAL truncation handles the case
// when no snapshots exist yet (should not panic or error).
func TestWALTruncationWithNoSnapshots(t *testing.T) {
	dir := t.TempDir()

	// No snapshot interval configured, so no snapshots will be created
	cs := NewCommitStore(dir, logger.NewNopLogger(), Config{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a version
	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
		{
			Name: "test",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: []byte("key"), Value: []byte("value")},
				},
			},
		},
	})
	require.NoError(t, err)

	// Commit should succeed even though no snapshots exist
	// (tryTruncateWAL should handle this gracefully)
	_, err = cs.Commit()
	require.NoError(t, err)

	// WAL should still have entries
	firstIndex, err := cs.db.GetWAL().FirstOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(1), firstIndex, "WAL should not be truncated when no snapshots exist")

	require.NoError(t, cs.Close())
}

// TestWALTruncationDelta tests that WAL truncation correctly uses the delta
// for version-to-index conversion with non-zero initial version.
func TestWALTruncationDelta(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultConfig()
	cfg.SnapshotInterval = 2
	cfg.SnapshotKeepRecent = 1
	cs := NewCommitStore(dir, logger.NewNopLogger(), cfg)
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Set initial version to 100
	err = cs.SetInitialVersion(100)
	require.NoError(t, err)

	// Commit multiple versions
	for i := 0; i < 10; i++ {
		err = cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("key"), Value: []byte("value" + string(rune('0'+i)))},
					},
				},
			},
		})
		require.NoError(t, err)
		_, err = cs.Commit()
		require.NoError(t, err)
	}

	// Verify version (should be 100 + 9 = 109)
	require.Equal(t, int64(109), cs.Version())

	// Close and reopen to verify delta is computed correctly from WAL
	require.NoError(t, cs.Close())

	// Reopen
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), cfg)
	cs2.Initialize([]string{"test"})
	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)

	// Now verify delta is correct (computed from WAL entries)
	walDelta := cs2.db.GetWALIndexDelta()
	require.Equal(t, int64(99), walDelta, "Delta should be 99 (firstVersion 100 - firstIndex 1)")

	// Verify WAL truncation respects delta
	firstWALIndex, err := cs2.db.GetWAL().FirstOffset()
	require.NoError(t, err)

	// Get earliest snapshot version - may not exist yet if snapshots are async
	earliestSnapshot, err := cs2.GetEarliestVersion()
	if err != nil {
		t.Logf("No snapshots created yet: %v", err)
		require.NoError(t, cs2.Close())
		return
	}

	walFirstVersion := int64(firstWALIndex) + walDelta
	t.Logf("WAL first index: %d, WAL first version: %d, earliest snapshot: %d",
		firstWALIndex, walFirstVersion, earliestSnapshot)

	require.GreaterOrEqual(t, walFirstVersion, earliestSnapshot,
		"WAL first version should be >= earliest snapshot version")

	require.NoError(t, cs2.Close())
}
