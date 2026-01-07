package sc

import (
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

func TestNewCommitStore(t *testing.T) {
	dir := t.TempDir()
	cfg := config.StateCommitConfig{
		ZeroCopy:         true,
		SnapshotInterval: 10,
	}

	cs := NewCommitStore(dir, logger.NewNopLogger(), cfg)
	require.NotNil(t, cs)
	require.NotNil(t, cs.logger)
	require.True(t, cs.opts.ZeroCopy)
	require.Equal(t, uint32(10), cs.opts.SnapshotInterval)
	require.True(t, cs.opts.CreateIfMissing)
}

func TestNewCommitStoreWithCustomDirectory(t *testing.T) {
	homeDir := t.TempDir()
	customDir := t.TempDir()
	cfg := config.StateCommitConfig{
		Directory: customDir,
	}

	cs := NewCommitStore(homeDir, logger.NewNopLogger(), cfg)
	require.NotNil(t, cs)
	require.Contains(t, cs.opts.Dir, customDir)
}

func TestInitialize(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})

	stores := []string{"store1", "store2", "store3"}
	cs.Initialize(stores)

	require.Equal(t, stores, cs.opts.InitialStores)
}

func TestCommitStoreBasicOperations(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// Load version 0 to initialize the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

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

	// Verify pending entry has the changesets
	require.Equal(t, changesets, cs.pendingLogEntry.Changesets)

	// Commit
	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)

	// Pending entry should be cleared after commit
	require.Nil(t, cs.pendingLogEntry.Changesets)
	require.Nil(t, cs.pendingLogEntry.Upgrades)

	// Version should be updated
	require.Equal(t, int64(1), cs.Version())
}

func TestApplyChangeSetsEmpty(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	// Empty changesets should be no-op
	err = cs.ApplyChangeSets(nil)
	require.NoError(t, err)
	require.Nil(t, cs.pendingLogEntry.Changesets)

	err = cs.ApplyChangeSets([]*proto.NamedChangeSet{})
	require.NoError(t, err)
	require.Nil(t, cs.pendingLogEntry.Changesets)
}

func TestApplyUpgrades(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	// Apply upgrades
	upgrades := []*proto.TreeNameUpgrade{
		{Name: "newstore1"},
		{Name: "newstore2"},
	}
	err = cs.ApplyUpgrades(upgrades)
	require.NoError(t, err)

	// Verify pending entry has the upgrades
	require.Equal(t, upgrades, cs.pendingLogEntry.Upgrades)

	// Apply more upgrades - should append
	moreUpgrades := []*proto.TreeNameUpgrade{
		{Name: "newstore3"},
	}
	err = cs.ApplyUpgrades(moreUpgrades)
	require.NoError(t, err)

	require.Len(t, cs.pendingLogEntry.Upgrades, 3)
}

func TestApplyUpgradesEmpty(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	// Empty upgrades should be no-op
	err = cs.ApplyUpgrades(nil)
	require.NoError(t, err)
	require.Nil(t, cs.pendingLogEntry.Upgrades)

	err = cs.ApplyUpgrades([]*proto.TreeNameUpgrade{})
	require.NoError(t, err)
	require.Nil(t, cs.pendingLogEntry.Upgrades)
}

func TestLoadVersionCopyExisting(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// First load to create the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// For the first commit, we need to include initial upgrades in pending entry
	// so they are written to WAL for replay
	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}

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
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

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
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test", "other"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	// Get existing module
	module := cs.GetModuleByName("test")
	require.NotNil(t, module)

	// Get non-existing module
	module = cs.GetModuleByName("nonexistent")
	require.Nil(t, module)
}

func TestExporterVersionValidation(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

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
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})

	// Negative version should fail
	_, err := cs.Importer(-1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")

	// Version > MaxUint32 should fail
	_, err = cs.Importer(math.MaxUint32 + 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
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

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		// First commit needs initial upgrades for WAL replay
		if i == 0 {
			cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
		}
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
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

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

		// Pending entry should be cleared
		require.Nil(t, cs.pendingLogEntry.Changesets)
	}

	require.Equal(t, int64(5), cs.Version())
}

func TestCommitWithUpgradesAndChangesets(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

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

	// Both should be in pending entry
	require.Len(t, cs.pendingLogEntry.Upgrades, 1)
	require.Len(t, cs.pendingLogEntry.Changesets, 1)

	// Commit
	version, err := cs.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(1), version)

	// Pending entry should be cleared
	require.Nil(t, cs.pendingLogEntry.Changesets)
	require.Nil(t, cs.pendingLogEntry.Upgrades)
}

func TestSetInitialVersion(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	defer cs.Close()

	// Set initial version
	err = cs.SetInitialVersion(100)
	require.NoError(t, err)
}

func TestGetVersions(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit a few versions
	for i := 0; i < 3; i++ {
		// First commit needs initial upgrades for WAL replay
		if i == 0 {
			cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
		}
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
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs2.Initialize([]string{"test"})

	latestVersion, err := cs2.GetLatestVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), latestVersion)
}

func TestCreateWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// createWAL should create a valid WAL instance
	wal, err := cs.createWAL()
	require.NoError(t, err)
	require.NotNil(t, wal)

	// WAL should be functional - write an entry
	entry := proto.ChangelogEntry{
		Version: 1,
		Upgrades: []*proto.TreeNameUpgrade{
			{Name: "test"},
		},
	}
	err = wal.Write(entry)
	require.NoError(t, err)

	// Clean up
	require.NoError(t, wal.Close())
}

func TestLoadVersionReadOnlyWithWALReplay(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// First load to create the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Write data with WAL entries
	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
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

	// The read-only copy should have its own WAL
	require.NotNil(t, roCommitStore.wal)

	// Clean up
	require.NoError(t, roCommitStore.Close())
	require.NoError(t, cs.Close())
}

func TestLoadVersionReadOnlyCreatesOwnWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// First load to create the DB
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit some data with WAL entries
	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
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
	require.NotNil(t, ro1.wal)
	require.NotNil(t, ro2.wal)
	require.NotSame(t, ro1.wal, ro2.wal)

	// Clean up
	require.NoError(t, ro1.Close())
	require.NoError(t, ro2.Close())
	require.NoError(t, cs.Close())
}

func TestWALPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// First session: write data
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Write and commit
	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
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
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs2.Initialize([]string{"test"})

	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)

	// Version should be restored via WAL replay
	require.Equal(t, int64(2), cs2.Version())

	// Data should be accessible
	tree := cs2.GetModuleByName("test")
	require.NotNil(t, tree)

	require.NoError(t, cs2.Close())
}

func TestRollbackWithWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit multiple versions
	for i := 0; i < 5; i++ {
		if i == 0 {
			cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
		}
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
	require.NotNil(t, cs.wal)

	// Rollback to version 3
	err = cs.Rollback(3)
	require.NoError(t, err)
	require.Equal(t, int64(3), cs.Version())

	// WAL should still exist after rollback
	require.NotNil(t, cs.wal)

	require.NoError(t, cs.Close())

	// Reopen and verify rollback persisted
	cs2 := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs2.Initialize([]string{"test"})

	_, err = cs2.LoadVersion(0, false)
	require.NoError(t, err)

	// Version should be 3 after replay
	require.Equal(t, int64(3), cs2.Version())

	require.NoError(t, cs2.Close())
}

func TestRollbackCreatesWALIfNeeded(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// Load and commit
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
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

	// WAL should be nil after close
	require.Nil(t, cs.wal)

	// Rollback should create a new WAL
	err = cs.Rollback(1)
	require.NoError(t, err)

	// WAL should be created
	require.NotNil(t, cs.wal)

	require.NoError(t, cs.Close())
}

func TestCloseReleasesWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// WAL should exist after load
	require.NotNil(t, cs.wal)

	// Close
	require.NoError(t, cs.Close())

	// WAL should be nil after close
	require.Nil(t, cs.wal)
	require.Nil(t, cs.db)
}

func TestLoadVersionReusesExistingWAL(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// First load
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// WAL should be created
	require.NotNil(t, cs.wal)

	// Commit some data
	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
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

	// WAL should still exist
	require.NotNil(t, cs.wal)

	// Version should be replayed
	require.Equal(t, int64(1), cs.Version())

	require.NoError(t, cs.Close())
}

func TestReadOnlyCopyCannotCommit(t *testing.T) {
	dir := t.TempDir()
	cs := NewCommitStore(dir, logger.NewNopLogger(), config.StateCommitConfig{})
	cs.Initialize([]string{"test"})

	// First load
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)

	// Commit initial data
	cs.pendingLogEntry.Upgrades = []*proto.TreeNameUpgrade{{Name: "test"}}
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
