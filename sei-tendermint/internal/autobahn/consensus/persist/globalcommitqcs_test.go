package persist

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func makeSequentialFullCommitQCs(
	committee *types.Committee,
	keys []types.SecretKey,
	n int,
) []*types.FullCommitQC {
	rng := utils.TestRng()
	qcs := make([]*types.FullCommitQC, n)
	prev := utils.None[*types.CommitQC]()
	for i := range n {
		blocks := map[types.LaneID][]*types.Block{}
		for range 3 {
			lane := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
			var b *types.Block
			if bs := blocks[lane]; len(bs) > 0 {
				parent := bs[len(bs)-1]
				b = types.NewBlock(lane, parent.Header().Next(), parent.Header().Hash(), types.GenPayload(rng))
			} else {
				b = types.NewBlock(
					lane,
					types.LaneRangeOpt(prev, lane).Next(),
					types.GenBlockHeaderHash(rng),
					types.GenPayload(rng),
				)
			}
			blocks[lane] = append(blocks[lane], b)
		}
		laneQCs := map[types.LaneID]*types.LaneQC{}
		var headers []*types.BlockHeader
		for _, lane := range committee.Lanes().All() {
			if bs := blocks[lane]; len(bs) > 0 {
				votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
				for _, k := range keys {
					votes = append(votes, types.Sign(k, types.NewLaneVote(bs[len(bs)-1].Header())))
				}
				laneQCs[lane] = types.NewLaneQC(votes)
				for _, b := range bs {
					headers = append(headers, b.Header())
				}
			}
		}
		viewSpec := types.ViewSpec{CommitQC: prev}
		leader := committee.Leader(viewSpec.View())
		var leaderKey types.SecretKey
		for _, k := range keys {
			if k.Public() == leader {
				leaderKey = k
				break
			}
		}
		proposal := utils.OrPanic1(types.NewProposal(
			leaderKey,
			committee,
			viewSpec,
			time.Now(),
			laneQCs,
			utils.None[*types.AppQC](),
		))
		votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
		for _, k := range keys {
			votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
		}
		cqc := types.NewCommitQC(votes)
		qcs[i] = types.NewFullCommitQC(cqc, headers)
		prev = utils.Some(cqc)
	}
	return qcs
}

func TestNewGlobalCommitQCPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.LoadedQCs()))
	require.Equal(t, types.GlobalBlockNumber(0), gp.LoadNext())
	require.NoError(t, gp.Close())
}

func TestNewGlobalCommitQCPersisterNoop(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 5)

	gp, err := NewGlobalCommitQCPersister(utils.None[string]())
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.LoadedQCs()))

	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	lastNext := qcs[len(qcs)-1].QC().GlobalRange().Next
	require.Equal(t, lastNext, gp.LoadNext())

	// Truncate past everything in no-op mode advances cursor.
	futureN := lastNext + 100
	require.NoError(t, gp.TruncateBefore(futureN))
	require.Equal(t, futureN, gp.LoadNext())
	require.NoError(t, gp.Close())
}

func TestGlobalCommitQCPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 5)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	lastNext := qcs[len(qcs)-1].QC().GlobalRange().Next
	require.Equal(t, lastNext, gp.LoadNext())
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedQCs()
	require.Equal(t, len(qcs), len(loaded))
	for i, lqc := range loaded {
		require.Equal(t, qcs[i].QC().GlobalRange().First, lqc.First)
	}
	require.Equal(t, lastNext, gp2.LoadNext())
	require.NoError(t, gp2.Close())
}

func TestGlobalCommitQCTruncateAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 5)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	// Truncate before the third QC's range start, which should remove
	// all QCs whose range is fully below that point.
	truncPoint := qcs[2].QC().GlobalRange().First
	require.NoError(t, gp.TruncateBefore(truncPoint))
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedQCs()
	// QCs 0 and 1 should be gone (their ranges are fully before truncPoint).
	// QC 2 should be the first one remaining.
	require.GreaterOrEqual(t, len(loaded), 1)
	require.Equal(t, qcs[2].QC().GlobalRange().First, loaded[0].First)
	require.NoError(t, gp2.Close())
}

func TestGlobalCommitQCTruncateAll(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 3)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	lastNext := qcs[len(qcs)-1].QC().GlobalRange().Next
	require.NoError(t, gp.TruncateBefore(lastNext+100))
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedQCs()
	require.Equal(t, 0, len(loaded))
	require.NoError(t, gp2.Close())
}

func TestGlobalCommitQCDuplicateIgnored(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 2)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	require.NoError(t, gp.PersistQC(qcs[0]))
	require.NoError(t, gp.PersistQC(qcs[0])) // duplicate
	require.Equal(t, qcs[0].QC().GlobalRange().Next, gp.LoadNext())
	require.NoError(t, gp.Close())
}

func TestGlobalCommitQCGapError(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 3)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	// Skip qcs[0] and try to persist qcs[1] directly.
	err = gp.PersistQC(qcs[1])
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")
	require.NoError(t, gp.Close())
}

func TestGlobalCommitQCTruncateBeforeNoop(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 3)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	// TruncateBefore(0) is a no-op.
	require.NoError(t, gp.TruncateBefore(0))
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedQCs()
	require.Equal(t, 3, len(loaded))
	require.NoError(t, gp2.Close())
}

func TestGlobalCommitQCContinueAfterReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 6)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	for _, qc := range qcs[:3] {
		require.NoError(t, gp.PersistQC(qc))
	}
	require.NoError(t, gp.Close())

	gp2, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded := gp2.LoadedQCs()
	require.Equal(t, 3, len(loaded))
	for _, qc := range qcs[3:] {
		require.NoError(t, gp2.PersistQC(qc))
	}
	require.Equal(t, qcs[5].QC().GlobalRange().Next, gp2.LoadNext())
	require.NoError(t, gp2.Close())

	gp3, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	loaded = gp3.LoadedQCs()
	require.Equal(t, 6, len(loaded))
	require.NoError(t, gp3.Close())
}

func TestGlobalCommitQCTruncateMidRange(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 3)
	qcs := makeSequentialFullCommitQCs(committee, keys, 5)

	gp, err := NewGlobalCommitQCPersister(utils.Some(dir))
	require.NoError(t, err)
	_ = gp.LoadedQCs()
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	// Truncate at a point inside the first QC's range.
	// The first QC should be kept because its range extends past truncPoint.
	gr0 := qcs[0].QC().GlobalRange()
	if gr0.Len() > 1 {
		midPoint := gr0.First + types.GlobalBlockNumber(gr0.Len()/2)
		require.NoError(t, gp.TruncateBefore(midPoint))
		require.NoError(t, gp.Close())

		gp2, err := NewGlobalCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		loaded := gp2.LoadedQCs()
		require.Equal(t, 5, len(loaded))
		require.NoError(t, gp2.Close())
	} else {
		require.NoError(t, gp.Close())
	}
}
