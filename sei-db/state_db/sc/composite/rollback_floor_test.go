package composite

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

const (
	rollbackFloorPreBlocks = 8
	rollbackFloorKickoff   = rollbackFloorPreBlocks + 1
	rollbackFloorBatch     = 4

	// noKickoff is a kickoff height past every block a test commits, so those blocks stay memiavl-only.
	noKickoff = rollbackFloorKickoff + 100
)

func openRollbackFloorStore(
	t *testing.T, dir string, mode types.WriteMode, batch int,
) *CompositeCommitStore {
	t.Helper()
	return openCompositeForRollback(t, dir, mode, batch, rollbackSnapSettings{
		memiavlInterval: 2,
		flatkvInterval:  2,
	})
}

func buildFixedModeMigrationStore(t *testing.T, dir string) *CompositeCommitStore {
	t.Helper()
	workload := newMigrationWorkload(0x5702)

	cs := openRollbackFloorStore(t, dir, types.MemiavlOnly, 0)
	for i := 0; i < rollbackFloorPreBlocks; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(30, 0, 0, 5, 0)))
		_, err := cs.Commit(cs.Version() + 1)
		require.NoError(t, err)
	}
	require.NoError(t, cs.Close())

	cs = openRollbackFloorStore(t, dir, types.MigrateEVM, rollbackFloorBatch)
	for i := 0; i < 6; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(8, 4, 1, 2, 1)))
		_, err := cs.Commit(cs.Version() + 1)
		require.NoError(t, err)
	}
	return cs
}

func TestCompositeRollbackBelowFlatKVFloorIsRefusedBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	cs := buildFixedModeMigrationStore(t, dir)

	target := int64(rollbackFloorKickoff - 2)
	latest := cs.Version()
	require.Equal(t, latest, cs.memIAVL.Version())
	require.Equal(t, latest, cs.loadFlatKV().Version())

	err := cs.Rollback(target)
	require.Error(t, err)
	require.ErrorIs(t, err, flatkv.ErrVersionUnreachable)
	require.Contains(t, err.Error(), "use state sync")
	require.Equal(t, latest, cs.memIAVL.Version())
	require.Equal(t, latest, cs.loadFlatKV().Version())
	require.NoError(t, cs.Close())

	reopened := openRollbackFloorStore(t, dir, types.MigrateEVM, rollbackFloorBatch)
	require.Equal(t, latest, reopened.Version())
	require.Equal(t, latest, reopened.memIAVL.Version())
	require.Equal(t, latest, reopened.loadFlatKV().Version())
	require.NoError(t, reopened.Close())
}

func TestCompositeHistoricalReadBelowFlatKVFloorErrorsWithoutPanic(t *testing.T) {
	dir := t.TempDir()
	cs := buildFixedModeMigrationStore(t, dir)
	defer func() { require.NoError(t, cs.Close()) }()

	target := int64(rollbackFloorKickoff - 2)
	_, err := cs.LoadVersionReadOnly(target)
	require.Error(t, err)
	require.ErrorIs(t, err, flatkv.ErrVersionUnreachable)

	// The migration had not started at the FlatKV snapshot above the target, so the export is memiavl-only.
	exp, err := cs.Exporter(target)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exp)
	require.NoError(t, exp.Close())
	require.NotContains(t, moduleNamesOf(items), keys.FlatKVStoreKey)

	for _, reachable := range []int64{rollbackFloorKickoff - 1, rollbackFloorKickoff, cs.Version()} {
		ro, err := cs.LoadVersionReadOnly(reachable)
		require.NoError(t, err, "version %d must stay reachable", reachable)
		require.NoError(t, ro.Close())
	}
}

func TestCompositeExportBelowFlatKVFloorAfterKickoffFailsLoud(t *testing.T) {
	dir := t.TempDir()
	workload := newMigrationWorkload(0x5703)
	cs := buildFixedModeMigrationStore(t, dir)
	defer func() { require.NoError(t, cs.Close()) }()
	for i := 0; i < 20; i++ {
		require.NoError(t, cs.ApplyChangeSets(workload.generateBlock(8, 4, 1, 2, 1)))
		_, err := cs.Commit(cs.Version() + 1)
		require.NoError(t, err)
	}
	flatKVStore, ok := cs.loadFlatKV().(*flatkv.CommitStore)
	require.True(t, ok)
	require.NoError(t, flatKVStore.FlushSnapshots())

	// The migration had started at this height, so FlatKV is in its AppHash and can't be left out.
	target := int64(rollbackFloorKickoff + 1)
	require.ErrorIs(t, cs.loadFlatKV().CheckVersionReachable(target), flatkv.ErrVersionUnreachable)

	_, err := cs.Exporter(target)
	require.Error(t, err)
	require.ErrorIs(t, err, flatkv.ErrVersionUnreachable)
}

func rollbackFloorBlocks(seed int64, last int64) map[int64][]*proto.NamedChangeSet {
	w := newMigrationWorkload(seed)
	blocks := make(map[int64][]*proto.NamedChangeSet, last)
	for h := int64(1); h <= last; h++ {
		blocks[h] = w.generateBlock(5, 5, 1, 2, 2)
	}
	return blocks
}

func beginRollbackFloorBlock(t *testing.T, cs *CompositeCommitStore, height int64, kickoff int64) {
	t.Helper()
	batch := 0
	if height >= kickoff {
		batch = rollbackFloorBatch
	}
	require.NoError(t, cs.SetMigrationBatchSize(batch))
	if batch > 0 && cs.loadWriteMode() == types.MemiavlOnly {
		require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	}
}

func commitRollbackFloorBlock(
	t *testing.T,
	cs *CompositeCommitStore,
	blocks map[int64][]*proto.NamedChangeSet,
	height int64,
	kickoff int64,
) {
	t.Helper()
	require.Equal(t, height-1, cs.Version())
	beginRollbackFloorBlock(t, cs, height, kickoff)
	require.NoError(t, cs.ApplyChangeSets(cloneChangeSets(t, blocks[height])))
	v, err := cs.Commit(height)
	require.NoError(t, err)
	require.Equal(t, height, v)
}

func runAutoMigration(
	t *testing.T,
	dir string,
	cfg config.StateCommitConfig,
	blocks map[int64][]*proto.NamedChangeSet,
	last int64,
	kickoff int64,
) (map[int64]*proto.CommitInfo, *CompositeCommitStore) {
	t.Helper()
	cs := openAutoStoreWithConfig(t, dir, cfg, 0)
	canonical := make(map[int64]*proto.CommitInfo, last)
	for h := int64(1); h <= last; h++ {
		commitRollbackFloorBlock(t, cs, blocks, h, kickoff)
		canonical[h] = cloneCommitInfo(cs.LastCommitInfo())
	}
	return canonical, cs
}

func TestCompositeAutoRollbackToSeededFloorCanKickoffAgain(t *testing.T) {
	const last = int64(14)
	blocks := rollbackFloorBlocks(0x5703, last)
	cfg := autoExportConfig()
	cfg.FlatKVConfig.SnapshotKeepRecent = 100

	canonical, src := runAutoMigration(t, t.TempDir(), cfg, blocks, last, rollbackFloorKickoff)
	require.NoError(t, src.Close())

	dir := t.TempDir()
	_, cs := runAutoMigration(t, dir, cfg, blocks, last, rollbackFloorKickoff)
	require.NoError(t, cs.Rollback(rollbackFloorKickoff-1))
	require.Equal(t, int64(rollbackFloorKickoff-1), cs.Version())

	for h := int64(rollbackFloorKickoff); h <= last; h++ {
		commitRollbackFloorBlock(t, cs, blocks, h, rollbackFloorKickoff)
		requireCommitInfoEqual(t, canonical[h], cs.LastCommitInfo(), fmt.Sprintf("replayed height %d", h))
	}
	require.NoError(t, cs.Close())
}

func TestCompositeAutoDiscardStaleIdleFlatKV(t *testing.T) {
	dir := t.TempDir()
	cfg := autoExportConfig()
	cfg.FlatKVConfig.SnapshotKeepRecent = 100
	blocks := rollbackFloorBlocks(0x5704, 13)

	cs := openAutoStoreWithConfig(t, dir, cfg, 0)
	for h := int64(1); h < rollbackFloorKickoff; h++ {
		commitRollbackFloorBlock(t, cs, blocks, h, noKickoff)
	}

	beginRollbackFloorBlock(t, cs, rollbackFloorKickoff, rollbackFloorKickoff)
	require.NotNil(t, cs.loadFlatKV())
	require.NoError(t, cs.ApplyChangeSets(cloneChangeSets(t, blocks[rollbackFloorKickoff])))
	require.NoError(t, cs.Close())

	cs = openAutoStoreWithConfig(t, dir, cfg, 0)
	require.Equal(t, int64(rollbackFloorKickoff-1), cs.Version())
	require.Nil(t, cs.loadFlatKV())
	for h := int64(rollbackFloorKickoff); h <= rollbackFloorKickoff+3; h++ {
		commitRollbackFloorBlock(t, cs, blocks, h, noKickoff)
	}
	require.NoError(t, cs.Close())

	cs = openAutoStoreWithConfig(t, dir, cfg, 0)
	require.Equal(t, int64(rollbackFloorKickoff+3), cs.Version())
	require.Nil(t, cs.loadFlatKV())
	require.False(t, utils.DirExists(utils.GetFlatKVPath(dir)))

	commitRollbackFloorBlock(t, cs, blocks, rollbackFloorKickoff+4, rollbackFloorKickoff+4)
	require.NotNil(t, cs.loadFlatKV())
	require.Equal(t, int64(rollbackFloorKickoff+4), cs.Version())
	require.NoError(t, cs.Close())
}

// A crash between the memIAVL and flatkv commits of the kickoff block leaves memIAVL at K and an idle
// flatkv at its seed K-1. That is a torn commit, not a stale seed: the restart must keep flatkv, roll
// memIAVL back to K-1, and replay K to the canonical AppHash.
func TestCompositeAutoTornKickoffCommitReplaysKickoffBlock(t *testing.T) {
	const last = int64(rollbackFloorKickoff + 3)
	blocks := rollbackFloorBlocks(0x5705, last)
	cfg := autoExportConfig()
	cfg.FlatKVConfig.SnapshotKeepRecent = 100

	canonical, src := runAutoMigration(t, t.TempDir(), cfg, blocks, last, rollbackFloorKickoff)
	require.NoError(t, src.Close())
	require.True(t, hasLattice(canonical[rollbackFloorKickoff]))

	dir := t.TempDir()
	cs := openAutoStoreWithConfig(t, dir, cfg, 0)
	for h := int64(1); h < rollbackFloorKickoff; h++ {
		commitRollbackFloorBlock(t, cs, blocks, h, rollbackFloorKickoff)
	}
	beginRollbackFloorBlock(t, cs, rollbackFloorKickoff, rollbackFloorKickoff)
	require.NoError(t, cs.ApplyChangeSets(cloneChangeSets(t, blocks[rollbackFloorKickoff])))
	committed, err := cs.memIAVL.Commit(rollbackFloorKickoff)
	require.NoError(t, err)
	require.Equal(t, int64(rollbackFloorKickoff), committed)
	require.Equal(t, int64(rollbackFloorKickoff-1), cs.loadFlatKV().Version())
	require.NoError(t, cs.Close())

	cs = openAutoStoreWithConfig(t, dir, cfg, 0)
	require.Equal(t, int64(rollbackFloorKickoff-1), cs.Version(), "memIAVL must be rolled back to the seed")
	require.True(t, utils.DirExists(utils.GetFlatKVPath(dir)), "a torn kickoff commit must keep flatkv")

	for h := int64(rollbackFloorKickoff); h <= last; h++ {
		commitRollbackFloorBlock(t, cs, blocks, h, rollbackFloorKickoff)
		requireCommitInfoEqual(t, canonical[h], cs.LastCommitInfo(), fmt.Sprintf("replayed height %d", h))
	}
	require.NoError(t, cs.Close())
}

func TestIsStaleSeed(t *testing.T) {
	require.False(t, isStaleSeed(9, 9), "matching versions")
	require.False(t, isStaleSeed(10, 9), "one block behind is a torn kickoff commit")
	require.True(t, isStaleSeed(11, 9), "more than one block behind")
	require.True(t, isStaleSeed(5, 9), "seed above memIAVL")
}

func TestCompositeAutoPreKickoffSnapshotReplaysThroughMigration(t *testing.T) {
	const (
		snapshotHeight = int64(5)
		kickoffHeight  = int64(10)
		lastHeight     = int64(20)
	)
	blocks := rollbackFloorBlocks(0xC0FFEE, lastHeight)
	cfg := autoExportConfig()
	cfg.FlatKVConfig.SnapshotKeepRecent = 100

	src := openAutoStoreWithConfig(t, t.TempDir(), cfg, 0)
	canonical := make(map[int64]*proto.CommitInfo, lastHeight)
	var items []exportedItem
	for h := int64(1); h <= lastHeight; h++ {
		commitRollbackFloorBlock(t, src, blocks, h, kickoffHeight)
		canonical[h] = cloneCommitInfo(src.LastCommitInfo())
		if h == snapshotHeight {
			exp, err := src.Exporter(h)
			require.NoError(t, err)
			items = drainCompositeExporter(t, exp)
			require.NoError(t, exp.Close())
		}
	}
	require.NotContains(t, moduleNamesOf(items), keys.FlatKVStoreKey)
	require.False(t, hasLattice(canonical[kickoffHeight-1]))
	require.True(t, hasLattice(canonical[kickoffHeight]))
	require.NoError(t, src.Close())

	cases := []struct {
		name                string
		crashInKickoffBlock bool
	}{
		{name: "clean", crashInKickoffBlock: false},
		{name: "crash_in_kickoff_block", crashInKickoffBlock: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dst := openAutoStoreWithConfig(t, dir, cfg, 0)
			require.NoError(t, dst.Close())
			imp, err := dst.Importer(snapshotHeight)
			require.NoError(t, err)
			replayImport(t, imp, items)
			require.NoError(t, imp.Close())
			require.Nil(t, dst.loadFlatKV())

			dst = openAutoStoreWithConfig(t, dir, cfg, 0)
			require.Equal(t, snapshotHeight, dst.Version())
			require.Equal(t, types.MemiavlOnly, dst.loadWriteMode())

			for h := snapshotHeight + 1; h <= lastHeight; h++ {
				if tc.crashInKickoffBlock && h == kickoffHeight {
					beginRollbackFloorBlock(t, dst, h, kickoffHeight)
					require.NotNil(t, dst.loadFlatKV())
					require.NoError(t, dst.ApplyChangeSets(cloneChangeSets(t, blocks[h])))
					require.NoError(t, dst.Close())
					dst = openAutoStoreWithConfig(t, dir, cfg, 0)
					require.Equal(t, h-1, dst.Version())
					require.Nil(t, dst.loadFlatKV())
				}
				commitRollbackFloorBlock(t, dst, blocks, h, kickoffHeight)
				requireCommitInfoEqual(t, canonical[h], dst.LastCommitInfo(), fmt.Sprintf("height %d", h))
			}
			require.NoError(t, dst.Close())
		})
	}
}
