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

// testDeleteCommitQCsBefore exercises the lock-free deleteBefore on
// commitQCState. Production code always goes through MaybePruneAndPersist,
// but tests need to truncate without an anchor QC to verify edge cases.
func testDeleteCommitQCsBefore(t *testing.T, cp *CommitQCPersister, idx types.RoadIndex, anchor utils.Option[*types.CommitQC]) {
	t.Helper()
	for s := range cp.state.Lock() {
		require.NoError(t, s.deleteBefore(idx, anchor))
		return
	}
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

	testDeleteCommitQCsBefore(t, cp, 3, noQC)
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

	qcs := makeSequentialCommitQCs(committee, keys, 2)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	testDeleteCommitQCsBefore(t, cp, 0, noQC)
	require.NoError(t, cp.Close())

	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
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
	cp, loaded, err := NewCommitQCPersister(utils.None[string]())
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, 0, len(loaded))

	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	qcs := makeSequentialCommitQCs(committee, keys, 1)
	testPersistCommitQC(t, cp, qcs[0])
	require.Equal(t, types.RoadIndex(1), cp.LoadNext())
	testDeleteCommitQCsBefore(t, cp, 0, noQC)
	require.NoError(t, cp.Close())
}

func TestCommitQCDeleteBeforePastAll(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}
	// next is 3; prune past everything. deleteBefore advances the cursor
	// to 10 and truncates the WAL.
	testDeleteCommitQCsBefore(t, cp, 10, noQC)
	require.Equal(t, types.RoadIndex(10), cp.LoadNext())

	// New writes starting from 10 should work.
	moreQCs := makeSequentialCommitQCs(committee, keys, 12)
	testPersistCommitQC(t, cp, moreQCs[10])
	testPersistCommitQC(t, cp, moreQCs[11])
	require.NoError(t, cp.Close())

	// Reopen — should see only the post-TruncateAll entries.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
	require.Equal(t, types.RoadIndex(10), loaded[0].Index)
	require.Equal(t, types.RoadIndex(11), loaded[1].Index)
}

// TestCommitQCDeleteBeforePastAllCrashRecovery simulates a crash between WAL
// TruncateAll and new write: on restart the WAL is empty but the anchor is far ahead.
// deleteBefore must still advance the cursor so subsequent persists succeed.
func TestCommitQCDeleteBeforePastAllCrashRecovery(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(committee, keys, 3)
	cp, _, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	for _, qc := range qcs {
		testPersistCommitQC(t, cp, qc)
	}

	// deleteBefore truncates the WAL (past all), then "crash" before writing.
	testDeleteCommitQCsBefore(t, cp, 10, noQC)
	require.NoError(t, cp.Close()) // simulate crash — no new QCs written

	// Restart: WAL is empty, next will be 0.
	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Empty(t, loaded)
	require.Equal(t, types.RoadIndex(0), cp2.LoadNext())

	// Second deleteBefore on the empty WAL must advance the cursor.
	testDeleteCommitQCsBefore(t, cp2, 10, noQC)
	require.Equal(t, types.RoadIndex(10), cp2.LoadNext())

	// Writing from index 10 should now succeed.
	moreQCs := makeSequentialCommitQCs(committee, keys, 12)
	testPersistCommitQC(t, cp2, moreQCs[10])
	testPersistCommitQC(t, cp2, moreQCs[11])
	require.NoError(t, cp2.Close())

	_, loaded, err = NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
	require.Equal(t, types.RoadIndex(10), loaded[0].Index)
	require.Equal(t, types.RoadIndex(11), loaded[1].Index)
}

// TestCommitQCDeleteBeforeWithAnchorRecovers verifies that passing an anchor
// QC to deleteBefore re-persists it after a WAL reset, so the caller doesn't
// need to handle crash recovery separately.
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

	// Truncate past all, then "crash".
	testDeleteCommitQCsBefore(t, cp, 10, noQC)
	require.NoError(t, cp.Close())

	// Restart: WAL is empty. Pass the anchor QC (index 4) through deleteBefore.
	cp2, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Empty(t, loaded)

	// deleteBefore advances cursor to 4, then re-persists qcs[4] via anchor.
	testDeleteCommitQCsBefore(t, cp2, 4, utils.Some(qcs[4]))
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
	testDeleteCommitQCsBefore(t, cp, 3, noQC)
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
	testDeleteCommitQCsBefore(t, cp, 3, noQC)

	// Pruning at or below the current first should be a no-op.
	testDeleteCommitQCsBefore(t, cp, 2, noQC)
	testDeleteCommitQCsBefore(t, cp, 3, noQC)
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
	testDeleteCommitQCsBefore(t, cp, 2, noQC)
	require.Equal(t, types.RoadIndex(8), cp.LoadNext())

	// Second prune: remove 2, 3, 4.
	testDeleteCommitQCsBefore(t, cp, 5, noQC)
	require.NoError(t, cp.Close())

	// Verify indices 5, 6, 7 survive.
	_, loaded, err := NewCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 3, len(loaded))
	require.Equal(t, types.RoadIndex(5), loaded[0].Index)
	require.Equal(t, types.RoadIndex(6), loaded[1].Index)
	require.Equal(t, types.RoadIndex(7), loaded[2].Index)
}
