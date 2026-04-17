package persist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

var noQC = utils.None[*types.CommitQC]()
var noCommitQCCB = utils.None[func(*types.CommitQC)]()

func testCommitQC(
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	laneQCs map[types.LaneID]*types.LaneQC,
	appQC utils.Option[*types.AppQC],
) *types.CommitQC {
	vs := types.ViewSpec{CommitQC: prev}
	leader := committee.Leader(vs.View())
	var leaderKey types.SecretKey
	for _, k := range keys {
		if k.Public() == leader {
			leaderKey = k
			break
		}
	}
	fullProposal := utils.OrPanic1(types.NewProposal(
		leaderKey,
		committee,
		vs,
		time.Now(),
		laneQCs,
		appQC,
	))
	vote := types.NewCommitVote(fullProposal.Proposal().Msg())
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewCommitQC(votes)
}

func makeSequentialCommitQCs(
	committee *types.Committee,
	keys []types.SecretKey,
	count int,
) []*types.CommitQC {
	var qcs []*types.CommitQC
	prev := utils.None[*types.CommitQC]()
	for range count {
		qc := testCommitQC(committee, keys, prev, nil, utils.None[*types.AppQC]())
		qcs = append(qcs, qc)
		prev = utils.Some(qc)
	}
	return qcs
}

// testPersistCommitQC persists a single CommitQC via the public API.
func testPersistCommitQC(t *testing.T, cp *CommitQCPersister, qc *types.CommitQC) {
	t.Helper()
	require.NoError(t, cp.MaybePruneAndPersist(
		utils.None[*types.CommitQC](),
		[]*types.CommitQC{qc},
		noCommitQCCB,
	))
}

// testDeleteCommitQCsBefore truncates the WAL below the anchor's index and
// re-persists the anchor for crash recovery.
func testDeleteCommitQCsBefore(t *testing.T, cp *CommitQCPersister, anchor *types.CommitQC) {
	t.Helper()
	for s := range cp.state.Lock() {
		require.NoError(t, s.deleteBefore(anchor))
		return
	}
}

// clearCommitQCWAL removes all WAL files to simulate a crash between
// WAL truncation and the subsequent anchor write.
func clearCommitQCWAL(t *testing.T, dir string) {
	t.Helper()
	walDir := filepath.Join(dir, commitqcsDir)
	require.NoError(t, os.RemoveAll(walDir))
	require.NoError(t, os.MkdirAll(walDir, 0700))
}

func TestNewCommitQCPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	cp, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, 0, len(loaded))
	require.Equal(t, types.RoadIndex(0), cp.LoadNext())

	fi, err := os.Stat(filepath.Join(dir, commitqcsDir))
	require.NoError(t, err)
	require.True(t, fi.IsDir())
	require.NoError(t, cp.Close())
}

func TestPersistCommitQCAndLoad(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)

	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}
	require.Equal(t, types.RoadIndex(3), cp.LoadNext())
	require.NoError(t, cp.Close())

	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, cp2)
	require.Equal(t, 3, len(loaded))
	for i, lqc := range loaded {
		require.Equal(t, types.RoadIndex(i), lqc.Index)
		require.NoError(t, utils.TestDiff(qcs[i], lqc.QC))
	}
	require.Equal(t, types.RoadIndex(3), cp2.LoadNext())
	require.NoError(t, cp2.Close())
}

func TestCommitQCDeleteBeforeRemovesOldKeepsNew(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	testDeleteCommitQCsBefore(t, cp, qcs[3])
	require.NoError(t, cp.Close())

	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded), "should have indices 3 and 4")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index)
	require.Equal(t, types.RoadIndex(4), loaded[1].Index)
}

func TestCommitQCDeleteBeforeZero(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)

	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs[:2] {
		testPersistCommitQC(t, cp, qc)
	}

	// deleteBefore with anchor at index 0 should persist the anchor
	// (which is a duplicate here) and leave everything intact.
	testDeleteCommitQCsBefore(t, cp, qcs[0])
	require.NoError(t, cp.Close())

	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))

	testPersistCommitQC(t, cp2, qcs[2])
	require.Equal(t, types.RoadIndex(3), cp2.LoadNext())
	require.NoError(t, cp2.Close())
}

func TestCommitQCPersistDuplicateIsNoOp(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	testPersistCommitQC(t, cp, qcs[0])
	testPersistCommitQC(t, cp, qcs[1])
	// Persisting qcs[0] again is a no-op (idx < next).
	testPersistCommitQC(t, cp, qcs[0])
	require.Equal(t, types.RoadIndex(2), cp.LoadNext())
	require.NoError(t, cp.Close())
}

func TestCommitQCPersistGapRejected(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	testPersistCommitQC(t, cp, qcs[0])
	testPersistCommitQC(t, cp, qcs[1])
	// Skip qcs[2], try to persist qcs[3] — should fail because idx(3) != next(2).
	err = cp.MaybePruneAndPersist(noQC, []*types.CommitQC{qcs[3]}, noCommitQCCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")
	require.NoError(t, cp.Close())
}

func TestLoadAllDetectsCommitQCGap(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
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
	committee, keys := types.GenCommittee(rng, 4)
	qcs := makeSequentialCommitQCs(committee, keys, 11)

	// Fresh no-op persister: prune with anchor at index 0 (idx==0,
	// s.next==0). Should persist the anchor and advance to 1.
	cp, loaded, err := NewCommitQCPersister(utils.None[string]())
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, 0, len(loaded))
	require.NoError(t, cp.MaybePruneAndPersist(
		utils.Some(qcs[0]),
		qcs[1:5],
		noCommitQCCB,
	))
	require.Equal(t, types.RoadIndex(5), cp.LoadNext())

	// Prune with a future anchor (index 8 > s.next=5). deleteBefore
	// advances s.next to 8, then persistCommitQC persists the anchor
	// and advances s.next to 9. The remaining QCs (9,10) follow.
	// Before the fix, deleteBefore in no-op mode wouldn't advance
	// s.next, so re-persisting the anchor QC would fail with "out of
	// sequence".
	require.NoError(t, cp.MaybePruneAndPersist(
		utils.Some(qcs[8]),
		qcs[9:],
		noCommitQCCB,
	))
	require.Equal(t, types.RoadIndex(11), cp.LoadNext())
	require.NoError(t, cp.Close())
}

func TestCommitQCDeleteBeforePastAll(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 12)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for i := range 3 {
		testPersistCommitQC(t, cp, qcs[i])
	}
	// next is 3; deleteBefore with anchor at 10 truncates the WAL,
	// advances the cursor to 10, and re-persists the anchor (next → 11).
	testDeleteCommitQCsBefore(t, cp, qcs[10])
	require.Equal(t, types.RoadIndex(11), cp.LoadNext())

	// New write starting from 11 should work.
	testPersistCommitQC(t, cp, qcs[11])
	require.NoError(t, cp.Close())

	// Reopen — should see only the post-TruncateAll entries.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
	require.Equal(t, types.RoadIndex(10), loaded[0].Index)
	require.Equal(t, types.RoadIndex(11), loaded[1].Index)
}

// TestCommitQCDeleteBeforePastAllCrashRecovery simulates a crash between WAL
// TruncateAll and the anchor write: on restart the WAL is empty and the anchor
// must re-establish the cursor so subsequent persists succeed.
func TestCommitQCDeleteBeforePastAllCrashRecovery(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 12)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for i := range 3 {
		testPersistCommitQC(t, cp, qcs[i])
	}
	require.NoError(t, cp.Close())

	// Simulate crash: clear the WAL as if TruncateAll completed but the
	// subsequent anchor write never happened.
	clearCommitQCWAL(t, dir)

	// Restart: WAL is empty, next will be 0.
	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Empty(t, loaded)
	require.Equal(t, types.RoadIndex(0), cp2.LoadNext())

	// MaybePruneAndPersist with anchor at 10 re-establishes the cursor
	// and appends new QCs.
	require.NoError(t, cp2.MaybePruneAndPersist(
		utils.Some(qcs[10]),
		[]*types.CommitQC{qcs[11]},
		noCommitQCCB,
	))
	require.Equal(t, types.RoadIndex(12), cp2.LoadNext())
	require.NoError(t, cp2.Close())

	_, loaded, err = NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
	require.Equal(t, types.RoadIndex(10), loaded[0].Index)
	require.Equal(t, types.RoadIndex(11), loaded[1].Index)
}

// TestCommitQCDeleteBeforeWithAnchorRecovers verifies that after a crash
// leaves the WAL empty, passing an anchor QC re-persists it and
// re-establishes the cursor for subsequent writes.
func TestCommitQCDeleteBeforeWithAnchorRecovers(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}
	require.NoError(t, cp.Close())

	// Simulate crash: clear WAL.
	clearCommitQCWAL(t, dir)

	// Restart: WAL is empty. Pass the anchor QC (index 4) through deleteBefore.
	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Empty(t, loaded)

	// deleteBefore advances cursor to 4, then re-persists qcs[4] via anchor.
	testDeleteCommitQCsBefore(t, cp2, qcs[4])
	require.Equal(t, types.RoadIndex(5), cp2.LoadNext())

	// Continue writing from 5.
	testPersistCommitQC(t, cp2, qcs[4]) // duplicate — no-op
	require.Equal(t, types.RoadIndex(5), cp2.LoadNext())
	require.NoError(t, cp2.Close())

	// Reopen — anchor QC should be on disk.
	_, loaded, err = NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 1, len(loaded))
	require.Equal(t, types.RoadIndex(4), loaded[0].Index)
	require.NoError(t, utils.TestDiff(qcs[4], loaded[0].QC))
}

func TestCommitQCDeleteBeforeThenPersistMore(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 6)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)

	// Persist 0..4, delete before 3, then persist 5.
	for i := range 5 {
		testPersistCommitQC(t, cp, qcs[i])
	}
	testDeleteCommitQCsBefore(t, cp, qcs[3])
	testPersistCommitQC(t, cp, qcs[5])
	require.NoError(t, cp.Close())

	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 3, len(loaded), "should have indices 3, 4, 5")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index)
	require.Equal(t, types.RoadIndex(4), loaded[1].Index)
	require.Equal(t, types.RoadIndex(5), loaded[2].Index)
}

func TestCommitQCDeleteBeforeAlreadyPruned(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 5)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	// Prune up to index 3.
	testDeleteCommitQCsBefore(t, cp, qcs[3])

	// Pruning at or below the current first should be a no-op.
	testDeleteCommitQCsBefore(t, cp, qcs[2])
	testDeleteCommitQCsBefore(t, cp, qcs[3])
	require.NoError(t, cp.Close())

	// Verify nothing extra was pruned.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded), "should still have indices 3 and 4")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index)
	require.Equal(t, types.RoadIndex(4), loaded[1].Index)
}

func TestCommitQCProgressiveDeleteBefore(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 8)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	// First prune: remove 0, 1.
	testDeleteCommitQCsBefore(t, cp, qcs[2])
	require.Equal(t, types.RoadIndex(8), cp.LoadNext())

	// Second prune: remove 2, 3, 4.
	testDeleteCommitQCsBefore(t, cp, qcs[5])
	require.NoError(t, cp.Close())

	// Verify indices 5, 6, 7 survive.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 3, len(loaded))
	require.Equal(t, types.RoadIndex(5), loaded[0].Index)
	require.Equal(t, types.RoadIndex(6), loaded[1].Index)
	require.Equal(t, types.RoadIndex(7), loaded[2].Index)
}
