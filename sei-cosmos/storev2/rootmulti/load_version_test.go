package rootmulti

// Tests for LoadVersion at a historical height — the `seid export --height N` path. A versioned load is a
// read operation: it must serve that height and leave the data directory exactly as it found it.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
)

// scLayout captures the on-disk state a versioned load used to rewind: which snapshot each backend's
// "current" points at, the set of retained snapshot directories, and flatkv's SNAPSHOT_BASE marker (whose
// removal forces a full working-dir re-clone). Transient readonly-* view directories are excluded — they are
// created and removed within a single load.
type scLayout struct {
	flatKVCurrent   string
	flatKVSnapshots []string
	flatKVSnapBase  string
	memIAVLCurrent  string
}

func readSCLayout(t *testing.T, dir string) scLayout {
	t.Helper()
	flatKVDir := utils.GetFlatKVPath(dir)
	memIAVLDir := utils.GetCosmosSCStorePath(dir)
	return scLayout{
		flatKVCurrent:   readLinkIfPresent(t, filepath.Join(flatKVDir, "current")),
		flatKVSnapshots: listSnapshotDirs(t, flatKVDir),
		flatKVSnapBase:  readFileIfPresent(t, filepath.Join(flatKVDir, "working", "SNAPSHOT_BASE")),
		memIAVLCurrent:  readLinkIfPresent(t, filepath.Join(memIAVLDir, "current")),
	}
}

func readLinkIfPresent(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return target
}

func readFileIfPresent(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is built from the test's own temp dir
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func listSnapshotDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "snapshot-") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// requireNoOrphanedViewDirs asserts every readonly-* working directory a versioned load created has been
// cleaned up. A leaked one would keep growing the data volume across repeated exports.
func requireNoOrphanedViewDirs(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(utils.GetFlatKVPath(dir))
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), "readonly-"),
			"a read-only view directory survived Close: %s", e.Name())
	}
}

// seedBlocks commits n blocks and returns their commit records.
func seedBlocks(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig, n int) []commitRecord {
	t.Helper()
	store, storeKeys := newTestRootMulti(t, dir, cfg)
	evmData := newEVMTestData(0x11)
	records := make([]commitRecord, 0, n)
	for block := 1; block <= n; block++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
	}
	require.NoError(t, store.Close())
	return records
}

// openAt reopens the store and loads it at ver (0 = latest). The caller owns the returned store.
func openAt(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig, ver int64) *Store {
	t.Helper()
	store := NewStore(dir, cfg, seidbconfig.StateStoreConfig{}, nil)
	for _, name := range storeNames {
		store.MountStoreWithDB(types.NewKVStoreKey(name), types.StoreTypeIAVL, nil)
	}
	require.NoError(t, store.LoadVersion(ver))
	return store
}

// TestLoadVersionAtHeightLeavesDataDirUntouched is the regression guard for the bug this split fixes: a
// versioned load used to take the writer lock, rewind the flatkv "current" symlink to the snapshot at or
// below the target and delete SNAPSHOT_BASE — mutating a node's data directory as a side effect of an export.
func TestLoadVersionAtHeightLeavesDataDirUntouched(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()
	// Snapshot flatkv every block and retain them, so the tip's "current" points well past the export
	// target. With the default 10k interval only snapshot-0 would exist and the symlink assertion below
	// could not observe a rewind.
	cfg.FlatKVConfig.SnapshotInterval = 1
	cfg.FlatKVConfig.SnapshotKeepRecent = 16
	records := seedBlocks(t, dir, cfg, 6)

	before := readSCLayout(t, dir)
	require.NotEmpty(t, before.flatKVCurrent, "fixture precondition: flatkv must have a current snapshot")
	require.Greater(t, len(before.flatKVSnapshots), 2,
		"fixture precondition: several flatkv snapshots must exist for a rewind to be observable")

	target := records[1].version // well below the tip
	store := openAt(t, dir, cfg, target)
	require.NoError(t, store.Close())

	require.Equal(t, before, readSCLayout(t, dir),
		"a versioned load is a read: it must not rewind or re-clone anything on disk")
	requireNoOrphanedViewDirs(t, dir)
}

// TestLoadVersionAtHeightServesThatVersion verifies the historical load actually answers at the requested
// height rather than silently landing somewhere else.
func TestLoadVersionAtHeightServesThatVersion(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()
	records := seedBlocks(t, dir, cfg, 5)

	target := records[2]
	store := openAt(t, dir, cfg, target.version)
	defer func() { require.NoError(t, store.Close()) }()

	require.Equal(t, target.version, store.LastCommitID().Version)
	require.Equal(t, target.hash, store.LastCommitID().Hash,
		"the historical load must reproduce the commit hash recorded at that height")
}

// TestLoadVersionAtHeightKeepsStoreCommittable verifies the node can still start and commit after an export
// at a historical height has run against the same directory. Before the split the export left "current"
// pointing backwards, so the next start replayed from there.
func TestLoadVersionAtHeightKeepsStoreCommittable(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()
	records := seedBlocks(t, dir, cfg, 4)
	tip := records[len(records)-1].version

	exported := openAt(t, dir, cfg, records[0].version)
	require.NoError(t, exported.Close())

	store, storeKeys := newTestRootMulti(t, dir, cfg)
	defer func() { require.NoError(t, store.Close()) }()
	require.Equal(t, tip, store.LastCommitID().Version, "the node must reopen at the tip, not at the export height")

	next := simulateBlock(t, store, storeKeys, int(tip+1), newEVMTestData(0x11))
	require.Equal(t, tip+1, next.version)
}

// TestCommitPanicsAfterLoadVersionAtHeight verifies a store loaded at a historical version refuses to commit
// rather than corrupting the chain from an old height.
func TestCommitPanicsAfterLoadVersionAtHeight(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()
	records := seedBlocks(t, dir, cfg, 3)

	store := openAt(t, dir, cfg, records[0].version)
	defer func() { require.NoError(t, store.Close()) }()

	require.PanicsWithValue(t,
		"cannot commit: the store was loaded read-only at a historical version",
		func() { store.Commit(true) })
}

// TestLoadVersionAndUpgradeRejectsUpgradesAtHeight verifies store upgrades cannot ride along with a
// historical load: applying them would mean mutating a height that is only being read.
func TestLoadVersionAndUpgradeRejectsUpgradesAtHeight(t *testing.T) {
	dir := t.TempDir()
	cfg := evmMigratedConfig()
	records := seedBlocks(t, dir, cfg, 3)

	store := NewStore(dir, cfg, seidbconfig.StateStoreConfig{}, nil)
	defer func() { require.NoError(t, store.Close()) }()
	for _, name := range storeNames {
		store.MountStoreWithDB(types.NewKVStoreKey(name), types.StoreTypeIAVL, nil)
	}

	err := store.LoadVersionAndUpgrade(records[0].version, &types.StoreUpgrades{Added: []string{"newstore"}})
	require.ErrorContains(t, err, "store upgrades cannot be applied to a historical load")
}

// TestLoadVersionAtPreFlatKVEraHeightUnderAutoMode covers the auto-layout hole in this file: every other test
// here uses a fixed write mode, for which the era classification short-circuits, so none of them exercise a
// historical load at a height that predates flatkv's history. Under auto the chain runs memiavl-only until the
// migration trigger materializes flatkv and seeds it at the current height; heights below that seam hold all
// their consensus data in memiavl and must stay loadable. The load runs against a never-loaded store, which is
// the shape `seid export --height N` produces, so the era answer cannot come from flatkv's in-memory
// bookkeeping.
func TestLoadVersionAtPreFlatKVEraHeightUnderAutoMode(t *testing.T) {
	dir := t.TempDir()
	cfg := autoModeConfig()

	store, storeKeys := newTestRootMulti(t, dir, cfg)
	evmData := newEVMTestData(0xD1)
	preEra := make([]commitRecord, 0, 3)
	for block := 1; block <= 3; block++ {
		preEra = append(preEra, simulateBlock(t, store, storeKeys, block, evmData))
	}

	// Raising the batch size above 0 is the migration trigger: it advances the auto store past memiavl-only,
	// materializing flatkv and seeding its history at the current height. That seam is the era boundary.
	require.NoError(t, store.SetMigrationBatchSize(100))
	var inEra commitRecord
	for block := 4; block <= 8; block++ {
		inEra = simulateCosmosOnlyBlock(t, store, storeKeys, block)
	}
	require.NoError(t, store.Close())

	// Pre-era height: must load and reproduce that height's commit hash, served by memiavl alone.
	target := preEra[1]
	preEraStore := openAt(t, dir, cfg, target.version)
	require.Equal(t, target.version, preEraStore.LastCommitID().Version)
	require.Equal(t, target.hash, preEraStore.LastCommitID().Hash,
		"a pre-flatkv-era height must reproduce the commit hash recorded at that height")
	require.NoError(t, preEraStore.Close())

	// In-era height: flatkv is still opened and still answers.
	inEraStore := openAt(t, dir, cfg, inEra.version)
	require.Equal(t, inEra.version, inEraStore.LastCommitID().Version)
	require.Equal(t, inEra.hash, inEraStore.LastCommitID().Hash,
		"an in-era height must still be served with flatkv loaded")
	require.NoError(t, inEraStore.Close())

	requireNoOrphanedViewDirs(t, dir)
}
