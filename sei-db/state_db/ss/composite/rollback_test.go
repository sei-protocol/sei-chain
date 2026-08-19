package composite

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/stretchr/testify/require"
)

func setupRollbackStore(t *testing.T, interval int64, keepRecent int) (*CompositeStateStore, string) {
	t.Helper()
	return setupRollbackStoreAt(t, t.TempDir(), interval, keepRecent)
}

// legacyLayoutHome is a home directory that already holds the pre-state_store
// database directory, which is what makes GetStateStorePath resolve to it.
func legacyLayoutHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(utils.LegacyStateStorePath(home, config.PebbleDBBackend), 0o750))
	return home
}

func setupRollbackStoreAt(t *testing.T, home string, interval int64, keepRecent int) (*CompositeStateStore, string) {
	t.Helper()
	store, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:            config.PebbleDBBackend,
		AsyncWriteBuffer:   100,
		KeepRecent:         100000,
		SnapshotEnable:     true,
		SnapshotInterval:   interval,
		SnapshotKeepRecent: keepRecent,
	}, home)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NotNil(t, store.snapshotMgr)
	return store, home
}

func rollbackChangeset(version int64) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name: "bank",
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("balance"), Value: []byte(fmt.Sprintf("v%d", version))},
			{Key: []byte(fmt.Sprintf("block-%d", version)), Value: []byte("present")},
		}},
	}}
}

func writeRollbackBlock(t *testing.T, store *CompositeStateStore, version int64) {
	t.Helper()
	commitBlock(t, store, version, rollbackChangeset(version))
}

// writeEmptyRollbackBlock commits a block with no changesets the way rootmulti
// does: the state store apply is skipped, so the block writes no changelog
// entry and only the version watermark moves.
func writeEmptyRollbackBlock(t *testing.T, store *CompositeStateStore, version int64) {
	t.Helper()
	require.NoError(t, store.SetLatestVersion(version))
	store.ScheduleSnapshot(version)
}

func TestRollbackRestoresSnapshotAndReplaysWALToTarget(t *testing.T) {
	store, _ := setupRollbackStore(t, 5, 1)
	for version := int64(1); version <= 8; version++ {
		writeRollbackBlock(t, store, version)
	}
	settle(t, store)
	require.Equal(t, int64(8), store.GetLatestVersion())

	require.NoError(t, store.Rollback(7))
	require.Equal(t, int64(7), store.GetLatestVersion())

	value, err := store.Get("bank", 100, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("v7"), value)

	has, err := store.Has("bank", 100, []byte("block-7"))
	require.NoError(t, err)
	require.True(t, has)
	has, err = store.Has("bank", 100, []byte("block-8"))
	require.NoError(t, err)
	require.False(t, has)
}

// TestRollbackToOldestRetainedSnapshot covers the target that retention leaves
// with nothing to replay: the snapshot is the target height, so every entry the
// WAL holds above the oldest snapshot's own is dropped by the rollback.
func TestRollbackToOldestRetainedSnapshot(t *testing.T) {
	store, home := setupRollbackStore(t, 2, 1)
	for version := int64(1); version <= 6; version++ {
		writeRollbackBlock(t, store, version)
		settle(t, store)
	}
	require.Equal(t, []int64{4, 6}, snapshotVersions(t, store))
	cfg := store.config
	require.NoError(t, store.Close())
	// Retention stops at the oldest snapshot's own entry rather than the one
	// after it, so a reader can still tell a gap above it from a pruned prefix.
	require.Equal(t, int64(4), firstChangelogVersion(t, home))

	store, err := NewCompositeStateStore(cfg, home)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ValidateRollback(4))
	require.NoError(t, store.Rollback(4))
	require.Equal(t, int64(4), store.GetLatestVersion())
	require.Equal(t, []int64{4}, snapshotVersions(t, store))

	value, err := store.Get("bank", 100, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("v4"), value)
	has, err := store.Has("bank", 100, []byte("block-5"))
	require.NoError(t, err)
	require.False(t, has)

	// The store stays writable, which is what a changelog left empty in place
	// would break: the WAL refuses such a log on the next open.
	writeRollbackBlock(t, store, 5)
	settle(t, store)
	require.Equal(t, int64(5), store.GetLatestVersion())
	require.NoError(t, store.Close())

	reopened, err := NewCompositeStateStore(cfg, home)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	require.Equal(t, int64(5), reopened.GetLatestVersion())
}

// TestRollbackReplaysAcrossAnEmptyBlock covers a changelog gap that is not a
// pruned prefix: version 4 wrote nothing, so replay onto snapshot 3 resumes at
// version 5 and is still complete.
func TestRollbackReplaysAcrossAnEmptyBlock(t *testing.T) {
	store, _ := setupRollbackStore(t, 3, 1)
	for version := int64(1); version <= 6; version++ {
		if version == 4 {
			writeEmptyRollbackBlock(t, store, version)
		} else {
			writeRollbackBlock(t, store, version)
		}
		settle(t, store)
	}
	require.Equal(t, []int64{3, 6}, snapshotVersions(t, store))
	require.Equal(t, int64(6), store.GetLatestVersion())

	require.NoError(t, store.ValidateRollback(5))
	require.NoError(t, store.Rollback(5))
	require.Equal(t, int64(5), store.GetLatestVersion())

	value, err := store.Get("bank", 100, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("v5"), value)
	has, err := store.Has("bank", 100, []byte("block-6"))
	require.NoError(t, err)
	require.False(t, has)
}

func snapshotVersions(t *testing.T, store *CompositeStateStore) []int64 {
	t.Helper()
	versions, err := sssnapshot.ListSnapshotVersions(
		cosmosSnapshotRoot(store.homeDir, store.dbHome, store.config))
	require.NoError(t, err)
	return versions
}

// pruneChangelogBeforeVersion drops the changelog prefix below version, the way
// snapshot-anchored retention does. The store must be closed.
func pruneChangelogBeforeVersion(t *testing.T, changelogPath string, version int64) {
	t.Helper()
	stream, err := wal.NewChangelogWAL(changelogPath, wal.Config{})
	require.NoError(t, err)
	defer func() { require.NoError(t, stream.Close()) }()
	first, err := stream.FirstOffset()
	require.NoError(t, err)
	last, err := stream.LastOffset()
	require.NoError(t, err)
	keep, err := wal.FindFirstOffsetAfterVersion(stream, first, last, version-1)
	require.NoError(t, err)
	require.NoError(t, stream.TruncateBefore(keep))
}

// firstChangelogVersion reports the version of the oldest retained changelog
// entry. The store must be closed: it opens a second handle on the same log.
func firstChangelogVersion(t *testing.T, home string) int64 {
	t.Helper()
	changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(home, config.PebbleDBBackend))
	stream, err := wal.NewChangelogWAL(changelogPath, wal.Config{})
	require.NoError(t, err)
	defer func() { require.NoError(t, stream.Close()) }()
	first, err := stream.FirstOffset()
	require.NoError(t, err)
	entry, err := stream.ReadAt(first)
	require.NoError(t, err)
	return entry.Version
}

// TestValidateRollbackAgreesWithRollback holds the pre-flight to its promise: a
// target it approves must not fail once the database has been swapped, and a
// target it refuses must be refused for the same reason.
func TestValidateRollbackAgreesWithRollback(t *testing.T) {
	for _, target := range []int64{1, 3, 4, 5, 6, 9} {
		t.Run(fmt.Sprintf("target %d", target), func(t *testing.T) {
			store, _ := setupRollbackStore(t, 2, 1)
			for version := int64(1); version <= 6; version++ {
				writeRollbackBlock(t, store, version)
				settle(t, store)
			}

			validateErr := store.ValidateRollback(target)
			rollbackErr := store.Rollback(target)
			if validateErr == nil {
				require.NoError(t, rollbackErr)
				require.Equal(t, target, store.GetLatestVersion())
				return
			}
			require.EqualError(t, rollbackErr, validateErr.Error())
		})
	}
}

// TestRollbackCrashRecoveryAtEverySwapStep stops the directory swap after each
// of its steps and reopens. Whichever way recovery resolves it, the changelog
// has to survive: it is the only copy of the versions above the snapshots, so
// losing it costs the node every later rollback as well as this one.
// TestValidateRollbackLeavesTheChangelogAlone pins that planning reads the
// changelog through the handle the live store already holds. A second handle
// over the same directory truncates a corrupt tail as it opens, which is a
// repair, and RollbackValidator promises not to change store state.
func TestValidateRollbackLeavesTheChangelogAlone(t *testing.T) {
	store, home := setupRollbackStore(t, 2, 1)
	for version := int64(1); version <= 6; version++ {
		writeRollbackBlock(t, store, version)
		settle(t, store)
	}

	changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(home, config.PebbleDBBackend))
	segment := lastChangelogSegment(t, changelogPath)
	corrupted := append(readFileBytes(t, segment), []byte("corrupt tail")...)
	require.NoError(t, os.WriteFile(segment, corrupted, 0o600))

	require.NoError(t, store.ValidateRollback(5))

	require.Equal(t, corrupted, readFileBytes(t, segment),
		"validation rewrote the changelog segment")
}

func lastChangelogSegment(t *testing.T, changelogPath string) string {
	t.Helper()
	entries, err := os.ReadDir(changelogPath)
	require.NoError(t, err)
	var last string
	for _, entry := range entries {
		// Segments are 20-digit offsets, the same shape the WAL itself scans for.
		if entry.IsDir() || len(entry.Name()) < 20 {
			continue
		}
		last = entry.Name()
	}
	require.NotEmpty(t, last, "changelog %q holds no segment", changelogPath)
	return filepath.Join(changelogPath, last)
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	return bz
}

// TestRollbackCrashRecoveryAtEverySwapStep stops the swap after each step and
// reopens. Both directory layouts are covered: the swap renames the live
// directory away, and on the legacy layout that rename is what GetStateStorePath
// reads to decide where the database lives, so a reopen in that window has to
// resolve back to the directory the rollback moved rather than to an empty one.
func TestRollbackCrashRecoveryAtEverySwapStep(t *testing.T) {
	for _, layout := range []struct {
		name string
		home func(*testing.T) string
	}{
		{"state_store layout", func(t *testing.T) string { return t.TempDir() }},
		{"legacy layout", legacyLayoutHome},
	} {
		t.Run(layout.name, func(t *testing.T) {
			runRollbackCrashRecoveryAtEverySwapStep(t, layout.home)
		})
	}
}

func runRollbackCrashRecoveryAtEverySwapStep(t *testing.T, newHome func(*testing.T) string) {
	const target = int64(7)
	names := restoreStepNames()
	// Recovery rolls forward from the step that moves the live database aside,
	// because from there on the restored database is the one on disk.
	rollsForwardFrom := restoreStepIndex(t, "move live database aside")

	for stop := range names {
		t.Run(fmt.Sprintf("after %s", names[stop]), func(t *testing.T) {
			cfg, home, dbHome := stageRollbackSwap(t, newHome(t), target, stop+1)

			reopened, err := NewCompositeStateStore(cfg, home)
			require.NoError(t, err)
			t.Cleanup(func() { _ = reopened.Close() })
			require.Equal(t, dbHome, reopened.dbHome,
				"the reopened store must resolve to the database the rollback moved")
			require.NoDirExists(t, rollbackTmpDir(dbHome))
			require.NoDirExists(t, rollbackBackupDir(dbHome))
			require.NoFileExists(t, rollbackTargetPath(dbHome))

			if stop >= rollsForwardFrom {
				require.Equal(t, target, reopened.GetLatestVersion())
				// v7 is only readable if the changelog survived the crash: the
				// restored snapshot is version 5, and 6 and 7 are replayed.
				value, err := reopened.Get("bank", 100, []byte("balance"))
				require.NoError(t, err)
				require.Equal(t, []byte("v7"), value)
				has, err := reopened.Has("bank", 100, []byte("block-8"))
				require.NoError(t, err)
				require.False(t, has)
				return
			}

			// The rollback was abandoned, so the pre-crash database is back and
			// the changelog is intact enough to roll back for real.
			require.Equal(t, int64(8), reopened.GetLatestVersion())
			require.NoError(t, reopened.Rollback(target))
			require.Equal(t, target, reopened.GetLatestVersion())
		})
	}
}

// TestRollbackTargetMarkerIsPublishedAtomically pins the shape of the write: the
// marker is staged under a temporary name and renamed into place, so a reader
// sees all of it or none of it, and no staging file is left for the next open to
// trip over. The fsyncs that make this survive a power loss cannot be observed
// from a test; the atomic publish they protect can.
func TestRollbackTargetMarkerIsPublishedAtomically(t *testing.T) {
	dbHome := t.TempDir()

	require.NoError(t, writeRollbackTarget(dbHome, 42))
	require.NoFileExists(t, rollbackTargetPath(dbHome)+".tmp")
	target, ok, err := readRollbackTarget(dbHome)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(42), target)

	require.NoError(t, clearRollbackTarget(dbHome))
	_, ok, err = readRollbackTarget(dbHome)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, clearRollbackTarget(dbHome), "clearing a cleared marker is not an error")
}

func restoreStepIndex(t *testing.T, name string) int {
	t.Helper()
	for i, stepName := range restoreStepNames() {
		if stepName == name {
			return i
		}
	}
	t.Fatalf("restore step %q not found", name)
	return 0
}

// stageRollbackSwap runs the first stop steps of the directory swap and returns
// the config, home, and database directory of the store it left behind.
func stageRollbackSwap(t *testing.T, home string, target int64, stop int) (config.StateStoreConfig, string, string) {
	t.Helper()
	store, home := setupRollbackStoreAt(t, home, 5, 1)
	for version := int64(1); version <= 8; version++ {
		writeRollbackBlock(t, store, version)
	}
	settle(t, store)
	plan, err := store.planRollback(target)
	require.NoError(t, err)
	cfg, dbHome := store.config, store.dbHome
	require.NoError(t, store.Close())

	for _, step := range restoreSteps(plan.snapshotDir, dbHome, target)[:stop] {
		require.NoError(t, step.run(), step.name)
	}
	return cfg, home, dbHome
}

func stageRollbackRestore(t *testing.T, target int64) (config.StateStoreConfig, string) {
	t.Helper()
	cfg, home, _ := stageRollbackSwap(t, t.TempDir(), target, len(restoreStepNames()))
	return cfg, home
}

func TestRollbackCrashRecoveryCompletesPendingRestore(t *testing.T) {
	t.Run("after restore before WAL cut", func(t *testing.T) {
		cfg, home := stageRollbackRestore(t, 7)

		reopened, err := NewCompositeStateStore(cfg, home)
		require.NoError(t, err)
		t.Cleanup(func() { _ = reopened.Close() })
		require.Equal(t, int64(7), reopened.GetLatestVersion())
		require.NoFileExists(t, rollbackTargetPath(reopened.dbHome))
	})

	t.Run("after WAL cut before reopen", func(t *testing.T) {
		cfg, home := stageRollbackRestore(t, 7)
		changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(home, config.PebbleDBBackend))
		require.NoError(t, truncateChangelogAfterVersion(changelogPath, 7))

		reopened, err := NewCompositeStateStore(cfg, home)
		require.NoError(t, err)
		t.Cleanup(func() { _ = reopened.Close() })
		require.Equal(t, int64(7), reopened.GetLatestVersion())
		require.NoFileExists(t, rollbackTargetPath(reopened.dbHome))
	})
}

func TestRollbackRejectsUnreachableTargets(t *testing.T) {
	t.Run("below first snapshot", func(t *testing.T) {
		store, _ := setupRollbackStore(t, 5, 1)
		for version := int64(1); version <= 5; version++ {
			writeRollbackBlock(t, store, version)
		}
		settle(t, store)

		err := store.Rollback(4)
		require.ErrorContains(t, err, "no snapshot at or below target")
	})

	t.Run("changelog cut below target", func(t *testing.T) {
		store, home := setupRollbackStore(t, 5, 1)
		for version := int64(1); version <= 8; version++ {
			writeRollbackBlock(t, store, version)
		}
		settle(t, store)
		cfg := store.config
		require.NoError(t, store.Close())

		changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(home, config.PebbleDBBackend))
		require.NoError(t, truncateChangelogAfterVersion(changelogPath, 5))

		reopened, err := NewCompositeStateStore(cfg, home)
		require.NoError(t, err)
		t.Cleanup(func() { _ = reopened.Close() })

		require.ErrorContains(t, reopened.Rollback(7), "changelog has no newer entries")
	})

	t.Run("changelog pruned past target", func(t *testing.T) {
		store, home := setupRollbackStore(t, 5, 1)
		for version := int64(1); version <= 8; version++ {
			writeRollbackBlock(t, store, version)
		}
		settle(t, store)
		cfg := store.config
		require.NoError(t, store.Close())

		// A changelog whose prefix was pruned away cannot bridge the gap between
		// the snapshot and the target.
		changelogPath := utils.GetChangelogPath(utils.GetStateStorePath(home, config.PebbleDBBackend))
		pruneChangelogBeforeVersion(t, changelogPath, 7)

		reopened, err := NewCompositeStateStore(cfg, home)
		require.NoError(t, err)
		t.Cleanup(func() { _ = reopened.Close() })

		require.ErrorContains(t, reopened.Rollback(8), "the changelog starts at version 7, above snapshot 5")
	})

	t.Run("target above the store version", func(t *testing.T) {
		store, _ := setupRollbackStore(t, 5, 1)
		for version := int64(1); version <= 8; version++ {
			writeRollbackBlock(t, store, version)
		}
		settle(t, store)

		require.ErrorContains(t, store.Rollback(9), "the store is at version 8")
	})

	t.Run("zero target", func(t *testing.T) {
		store, _ := setupRollbackStore(t, 5, 1)
		require.ErrorContains(t, store.Rollback(0), "invalid state store rollback target")
	})
}

func TestRollbackRefusesEVMSplit(t *testing.T) {
	home := t.TempDir()
	store, err := NewCompositeStateStore(config.StateStoreConfig{
		Backend:          config.PebbleDBBackend,
		AsyncWriteBuffer: 100,
		KeepRecent:       100000,
		EVMSplit:         true,
		EVMDBDirectory:   filepath.Join(home, "evm_ss"),
		SnapshotEnable:   true,
		SnapshotInterval: 5,
	}, home)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.ErrorContains(t, store.Rollback(5), "evm-ss-split")
}

// TestSnapshotRetentionPrunesWALToOldestSnapshot pins where retention stops:
// at the oldest retained snapshot's own entry, not at the first one above it.
// Keeping that entry is what lets a later rollback tell a gap left by an empty
// block from a prefix that was pruned away.
func TestSnapshotRetentionPrunesWALToOldestSnapshot(t *testing.T) {
	store, home := setupRollbackStore(t, 2, 1)
	for version := int64(1); version <= 6; version++ {
		writeRollbackBlock(t, store, version)
		if version%2 == 0 {
			settle(t, store)
		}
	}
	settle(t, store)
	require.Equal(t, []int64{4, 6}, snapshotVersions(t, store))
	require.NoError(t, store.Close())

	require.Equal(t, int64(4), firstChangelogVersion(t, home))
}

// TestRollbackAcrossAnEmptyBlockAfterTheOldestSnapshot is the case the retained
// entry buys: the block right after the oldest retained snapshot wrote nothing,
// so the first entry above that snapshot is two versions up. Without an entry at
// or below the snapshot, that gap is indistinguishable from a pruned prefix and
// the rollback would be refused.
func TestRollbackAcrossAnEmptyBlockAfterTheOldestSnapshot(t *testing.T) {
	store, home := setupRollbackStore(t, 3, 1)
	for version := int64(1); version <= 9; version++ {
		if version == 7 {
			writeEmptyRollbackBlock(t, store, version)
		} else {
			writeRollbackBlock(t, store, version)
		}
		settle(t, store)
	}
	// Retention drops snapshot 3 and prunes the changelog to snapshot 6's own
	// entry. Version 7 wrote nothing, so the first entry above it is version 8.
	require.Equal(t, []int64{6, 9}, snapshotVersions(t, store))
	require.Equal(t, int64(6), firstChangelogVersion(t, home))

	require.NoError(t, store.ValidateRollback(8))
	require.NoError(t, store.Rollback(8))
	require.Equal(t, int64(8), store.GetLatestVersion())

	value, err := store.Get("bank", 100, []byte("balance"))
	require.NoError(t, err)
	require.Equal(t, []byte("v8"), value)
	has, err := store.Has("bank", 100, []byte("block-9"))
	require.NoError(t, err)
	require.False(t, has)
}
