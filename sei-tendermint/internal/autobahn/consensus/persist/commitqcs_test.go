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

func testCommitQC(
	rng utils.Rng,
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
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	count int,
) []*types.CommitQC {
	var qcs []*types.CommitQC
	prev := utils.None[*types.CommitQC]()
	for range count {
		qc := testCommitQC(rng, committee, keys, prev, nil, utils.None[*types.AppQC]())
		qcs = append(qcs, qc)
		prev = utils.Some(qc)
	}
	return qcs
}

func TestNewCommitQCPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	cp, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, 0, len(loaded))
	require.Equal(t, types.RoadIndex(0), cp.LoadNext())

	fi, err := os.Stat(filepath.Join(dir, "commitqcs"))
	require.NoError(t, err)
	require.True(t, fi.IsDir())
}

func TestPersistCommitQCAndLoad(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 3)

	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)

	for _, qc := range qcs {
		require.NoError(t, cp.PersistCommitQC(qc))
	}
	require.Equal(t, types.RoadIndex(3), cp.LoadNext())

	cp2, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.NotNil(t, cp2)
	require.Equal(t, 3, len(loaded))
	for i, lqc := range loaded {
		require.Equal(t, types.RoadIndex(i), lqc.Index)
		require.NoError(t, utils.TestDiff(qcs[i], lqc.QC))
	}
	require.Equal(t, types.RoadIndex(3), cp2.LoadNext())
}

func TestLoadSkipsCorruptCommitQCFile(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 1)
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.NoError(t, cp.PersistCommitQC(qcs[0]))

	// Write a corrupt file for index 1
	corruptPath := filepath.Join(dir, "commitqcs", commitQCFilename(1))
	require.NoError(t, os.WriteFile(corruptPath, []byte("corrupt"), 0600))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 1, len(loaded), "should only load valid commitqc")
	require.NoError(t, utils.TestDiff(qcs[0], loaded[0].QC))
}

func TestLoadCommitQCTruncatesAtGap(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 4)
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)

	// Persist 0, 1, skip 2, persist 3 → gap at 2 → contiguous prefix [0, 1]
	require.NoError(t, cp.PersistCommitQC(qcs[0]))
	require.NoError(t, cp.PersistCommitQC(qcs[1]))
	require.NoError(t, cp.PersistCommitQC(qcs[3]))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded), "should have contiguous prefix [0, 1]")
	require.Equal(t, types.RoadIndex(0), loaded[0].Index)
	require.Equal(t, types.RoadIndex(1), loaded[1].Index)
}

func TestLoadCommitQCCorruptMidSequenceTruncatesAtGap(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 3)
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)

	// Persist 0, corrupt 1, persist 2 → gap after skipping corrupt → prefix [0]
	require.NoError(t, cp.PersistCommitQC(qcs[0]))
	require.NoError(t, cp.PersistCommitQC(qcs[2]))
	corruptPath := filepath.Join(dir, "commitqcs", commitQCFilename(1))
	require.NoError(t, os.WriteFile(corruptPath, []byte("corrupt"), 0600))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 1, len(loaded), "corrupt mid-sequence creates gap; only index 0 survives")
	require.Equal(t, types.RoadIndex(0), loaded[0].Index)
}

func TestLoadCommitQCSkipsMismatchedIndex(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 2)
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)

	// Persist qc[0] (index 0) but save it under filename for index 5
	require.NoError(t, cp.PersistCommitQC(qcs[0]))
	oldPath := filepath.Join(dir, "commitqcs", commitQCFilename(0))
	newPath := filepath.Join(dir, "commitqcs", commitQCFilename(5))
	require.NoError(t, os.Rename(oldPath, newPath))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 0, len(loaded), "mismatched index should be skipped")
}

func TestLoadCommitQCSkipsUnrecognizedFilename(t *testing.T) {
	dir := t.TempDir()
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	_ = cp

	qcDir := filepath.Join(dir, "commitqcs")
	require.NoError(t, os.WriteFile(filepath.Join(qcDir, "notaqc.pb"), []byte("data"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(qcDir, "readme.txt"), []byte("hi"), 0600))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 0, len(loaded))
}

func TestCommitQCDeleteBeforeRemovesOldKeepsNew(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 5)
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	for _, qc := range qcs {
		require.NoError(t, cp.PersistCommitQC(qc))
	}

	require.NoError(t, cp.DeleteBefore(3))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded), "should have indices 3 and 4")
	require.Equal(t, types.RoadIndex(3), loaded[0].Index)
	require.Equal(t, types.RoadIndex(4), loaded[1].Index)
}

func TestCommitQCDeleteBeforeZero(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	dir := t.TempDir()

	qcs := makeSequentialCommitQCs(rng, committee, keys, 2)
	cp, _, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	for _, qc := range qcs {
		require.NoError(t, cp.PersistCommitQC(qc))
	}

	// idx=0 should be a no-op
	require.NoError(t, cp.DeleteBefore(0))

	_, loaded, err := NewCommitQCPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 2, len(loaded))
}

func TestCommitQCFilenameRoundTrip(t *testing.T) {
	idx := types.RoadIndex(42)
	name := commitQCFilename(idx)
	parsed, err := parseCommitQCFilename(name)
	require.NoError(t, err)
	require.Equal(t, idx, parsed)
}
