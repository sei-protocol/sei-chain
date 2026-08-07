package persist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func testCommitQC(
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	laneQCs map[types.LaneID]*types.LaneQC,
) *types.CommitQC {
	ep := types.NewEpoch(0, types.OpenRoadRange(), time.Time{}, committee, 0)
	return types.BuildCommitQC(ep, keys, prev, laneQCs)
}

func makeSequentialCommitQCs(
	committee *types.Committee,
	keys []types.SecretKey,
	count int,
) []*types.CommitQC {
	var qcs []*types.CommitQC
	prev := utils.None[*types.CommitQC]()
	for range count {
		qc := testCommitQC(committee, keys, prev, nil)
		qcs = append(qcs, qc)
		prev = utils.Some(qc)
	}
	return qcs
}

// testPersistCommitQC persists a single CommitQC via the public API.
func testPersistCommitQC(t *testing.T, cp *CommitQCPersister, qc *types.CommitQC) {
	t.Helper()
	require.NoError(t, cp.Persist(0, []*types.CommitQC{qc}))
}

func testDeleteCommitQCsBefore(t *testing.T, cp *CommitQCPersister, idx types.RoadIndex) {
	t.Helper()
	require.NoError(t, cp.Persist(idx, nil))
}

func TestNewCommitQCPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	cp, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, 0, len(loaded))
	require.Equal(t, types.RoadIndex(0), cp.Next())

	fi, err := os.Stat(filepath.Join(dir, commitqcsDir))
	require.NoError(t, err)
	require.True(t, fi.IsDir())
	require.NoError(t, cp.Close())
}

func TestPersistCommitQCAndLoad(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)

	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}
	require.Equal(t, types.RoadIndex(3), cp.Next())
	require.NoError(t, cp.Close())

	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, cp2)
	require.Equal(t, 3, len(loaded))
	for i, lqc := range loaded {
		require.Equal(t, types.RoadIndex(i), lqc.Index())
		require.NoError(t, utils.TestDiff(qcs[i], lqc))
	}
	require.Equal(t, types.RoadIndex(3), cp2.Next())
	require.NoError(t, cp2.Close())
}

func TestCommitQCDeleteBeforeRemovesOldKeepsNew(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	testDeleteCommitQCsBefore(t, cp, qcs[3].Index())
	require.NoError(t, cp.Close())

	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded), "should have indices 3 and 4")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index())
	require.Equal(t, types.RoadIndex(4), loaded[1].Index())
}

func TestCommitQCDeleteBeforeZero(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)

	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs[:2] {
		testPersistCommitQC(t, cp, qc)
	}

	// deleteBefore with index 0 should leave everything intact.
	testDeleteCommitQCsBefore(t, cp, qcs[0].Index())
	require.NoError(t, cp.Close())

	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))

	testPersistCommitQC(t, cp2, qcs[2])
	require.Equal(t, types.RoadIndex(3), cp2.Next())
	require.NoError(t, cp2.Close())
}

func TestCommitQCPersistDuplicateIsNoOp(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	testPersistCommitQC(t, cp, qcs[0])
	testPersistCommitQC(t, cp, qcs[1])
	// Persisting qcs[0] again is a no-op (idx < next).
	testPersistCommitQC(t, cp, qcs[0])
	require.Equal(t, types.RoadIndex(2), cp.Next())
	require.NoError(t, cp.Close())
}

func TestCommitQCPersistGapRejected(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	testPersistCommitQC(t, cp, qcs[0])
	testPersistCommitQC(t, cp, qcs[1])
	// Skip qcs[2], try to persist qcs[3] — should fail because idx(3) != next(2).
	err = cp.Persist(0, []*types.CommitQC{qcs[3]})
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")
	require.NoError(t, cp.Close())
}

func TestLoadAllDetectsCommitQCGap(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	// Build 3 sequential CommitQCs (indices 0, 1, 2).
	qcs := makeSequentialCommitQCs(committee, keys, 3)

	// Write directly to the WAL, bypassing the contiguity check to simulate
	// on-disk corruption (index 0 then index 2, skipping 1).
	walDir := filepath.Join(dir, commitqcsDir)
	require.NoError(t, os.MkdirAll(walDir, 0700))
	iw, err := openIndexedWAL(walDir, types.CommitQCConv)
	require.NoError(t, err)
	require.NoError(t, iw.Write(qcs[0]))
	require.NoError(t, iw.Write(qcs[2]))
	require.NoError(t, iw.Close())

	_, _, err = NewCommitQCPersister(utils.Some(dir))
	require.Error(t, err)
	require.Contains(t, err.Error(), "gap")
}

func TestNoOpCommitQCPersister(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	qcs := makeSequentialCommitQCs(committee, keys, 11)

	// Fresh no-op persister: persist sequential QCs and track Next.
	cp, loaded, err := NewCommitQCPersister(utils.None[string]())
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, 0, len(loaded))
	require.NoError(t, cp.Persist(0, qcs[:5]))
	require.Equal(t, types.RoadIndex(5), cp.Next())

	// Prune with a future index. deleteBefore advances persisted.Next,
	// so the remaining QCs follow the new bound.
	require.NoError(t, cp.Persist(8, qcs[8:]))
	require.Equal(t, types.RoadIndex(11), cp.Next())
	require.NoError(t, cp.Close())
}

func TestCommitQCDeleteBeforePastAll(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 12)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for i := range 3 {
		testPersistCommitQC(t, cp, qcs[i])
	}
	// next is 3; deleteBefore at 10 truncates the WAL and advances the
	// cursor to 10.
	testDeleteCommitQCsBefore(t, cp, qcs[10].Index())
	require.Equal(t, types.RoadIndex(10), cp.Next())

	// New writes starting from 10 should work.
	testPersistCommitQC(t, cp, qcs[10])
	testPersistCommitQC(t, cp, qcs[11])
	require.NoError(t, cp.Close())

	// Reopen — should see only the post-TruncateAll entries.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
	require.Equal(t, types.RoadIndex(10), loaded[0].Index())
	require.Equal(t, types.RoadIndex(11), loaded[1].Index())
}

func TestCommitQCDeleteBeforeThenPersistMore(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 6)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	// Persist 0..4, delete before 3, then persist 5.
	for i := range 5 {
		testPersistCommitQC(t, cp, qcs[i])
	}
	testDeleteCommitQCsBefore(t, cp, qcs[3].Index())
	testPersistCommitQC(t, cp, qcs[5])
	require.NoError(t, cp.Close())

	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 3, len(loaded), "should have indices 3, 4, 5")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index())
	require.Equal(t, types.RoadIndex(4), loaded[1].Index())
	require.Equal(t, types.RoadIndex(5), loaded[2].Index())
}

func TestCommitQCDeleteBeforeAlreadyPruned(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	// Prune up to index 3.
	testDeleteCommitQCsBefore(t, cp, qcs[3].Index())

	// Pruning at or below the current first should be a no-op.
	testDeleteCommitQCsBefore(t, cp, qcs[2].Index())
	testDeleteCommitQCsBefore(t, cp, qcs[3].Index())
	require.NoError(t, cp.Close())

	// Verify nothing extra was pruned.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded), "should still have indices 3 and 4")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index())
	require.Equal(t, types.RoadIndex(4), loaded[1].Index())
}

func TestCommitQCProgressiveDeleteBefore(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 4)
	committee := registry.LatestEpoch().Committee()
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 8)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	// First prune: remove 0, 1.
	testDeleteCommitQCsBefore(t, cp, qcs[2].Index())
	require.Equal(t, types.RoadIndex(8), cp.Next())

	// Second prune: remove 2, 3, 4.
	testDeleteCommitQCsBefore(t, cp, qcs[5].Index())
	require.NoError(t, cp.Close())

	// Verify indices 5, 6, 7 survive.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 3, len(loaded))
	require.Equal(t, types.RoadIndex(5), loaded[0].Index())
	require.Equal(t, types.RoadIndex(6), loaded[1].Index())
	require.Equal(t, types.RoadIndex(7), loaded[2].Index())
}
