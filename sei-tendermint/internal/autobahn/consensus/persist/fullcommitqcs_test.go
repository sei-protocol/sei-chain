package persist

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func makeSequentialFullCommitQCs(
	rng utils.Rng,
	registry *epoch.Registry,
	keys []types.SecretKey,
	n int,
) []*types.FullCommitQC {
	qcs := make([]*types.FullCommitQC, n)
	prev := utils.None[*types.CommitQC]()
	for i := range n {
		vs := types.ViewSpec{CommitQC: prev}
		committee := registry.EpochFor(vs.View().Index).Committee()
		lane := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
		b := types.NewBlock(lane, types.LaneRangeOpt(prev, lane).Next(), types.GenBlockHeaderHash(rng), types.GenPayload(rng))
		lv := types.NewLaneVote(b.Header())
		lvotes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
		for _, k := range keys {
			lvotes = append(lvotes, types.Sign(k, lv))
		}
		laneQCs := map[types.LaneID]*types.LaneQC{lane: types.NewLaneQC(lvotes, 0)}
		cqc := types.BuildCommitQC(committee, keys, prev, registry.FirstBlock(), time.Time{}, laneQCs, utils.None[*types.AppQC]())
		qcs[i] = types.NewFullCommitQC(cqc, []*types.BlockHeader{b.Header()})
		prev = utils.Some(qcs[i].QC())
	}
	return qcs
}

func TestNewFullCommitQCPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	gp, err := NewFullCommitQCPersister(utils.Some(dir), 0)
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.ConsumeLoaded()))
	require.Equal(t, types.GlobalBlockNumber(0), gp.Next())
	require.NoError(t, gp.Close())
}

func TestNewFullCommitQCPersisterNoop(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 5)

	gp, err := NewFullCommitQCPersister(utils.None[string](), registry.FirstBlock())
	require.NoError(t, err)
	require.NotNil(t, gp)
	require.Equal(t, 0, len(gp.ConsumeLoaded()))

	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	lastNext := qcs[len(qcs)-1].QC().GlobalRange().Next
	require.Equal(t, lastNext, gp.Next())

	// Truncate past everything in no-op mode advances cursor.
	futureN := lastNext + 100
	require.NoError(t, gp.TruncateBefore(futureN))
	require.Equal(t, futureN, gp.Next())
	require.NoError(t, gp.Close())
}

func TestFullCommitQCPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 5)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	lastNext := qcs[len(qcs)-1].QC().GlobalRange().Next
	require.Equal(t, lastNext, gp.Next())
	require.NoError(t, gp.Close())

	gp2, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	loaded := gp2.ConsumeLoaded()
	require.Equal(t, len(qcs), len(loaded))
	for i, lqc := range loaded {
		require.Equal(t, qcs[i].QC().GlobalRange().First, lqc.QC().GlobalRange().First)
	}
	require.Equal(t, lastNext, gp2.Next())
	require.NoError(t, gp2.Close())
}

func TestFullCommitQCTruncateAndReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 5)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	// Truncate before the third QC's range start, which should remove
	// all QCs whose range is fully below that point.
	truncPoint := qcs[2].QC().GlobalRange().First
	require.NoError(t, gp.TruncateBefore(truncPoint))
	require.NoError(t, gp.Close())

	gp2, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	loaded := gp2.ConsumeLoaded()
	// QCs 0 and 1 should be gone (their ranges are fully before truncPoint).
	// QC 2 should be the first one remaining.
	require.GreaterOrEqual(t, len(loaded), 1)
	require.Equal(t, qcs[2].QC().GlobalRange().First, loaded[0].QC().GlobalRange().First)
	require.NoError(t, gp2.Close())
}

func TestFullCommitQCTruncateAll(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 3)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	lastNext := qcs[len(qcs)-1].QC().GlobalRange().Next
	require.NoError(t, gp.TruncateBefore(lastNext+100))
	require.NoError(t, gp.Close())

	gp2, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	require.Equal(t, 0, len(gp2.ConsumeLoaded()))
	require.NoError(t, gp2.Close())
}

func TestFullCommitQCDuplicateIgnored(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 2)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	require.NoError(t, gp.PersistQC(qcs[0]))
	require.NoError(t, gp.PersistQC(qcs[0])) // duplicate
	require.Equal(t, qcs[0].QC().GlobalRange().Next, gp.Next())
	require.NoError(t, gp.Close())
}

func TestFullCommitQCGapError(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 3)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	// Skip qcs[0] and try to persist qcs[1] directly.
	err = gp.PersistQC(qcs[1])
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")
	require.NoError(t, gp.Close())
}

func TestFullCommitQCTruncateBeforeNoop(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 3)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	for _, qc := range qcs {
		require.NoError(t, gp.PersistQC(qc))
	}
	// TruncateBefore(0) is a no-op.
	require.NoError(t, gp.TruncateBefore(0))
	require.NoError(t, gp.Close())

	gp2, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	require.Equal(t, 3, len(gp2.ConsumeLoaded()))
	require.NoError(t, gp2.Close())
}

func TestFullCommitQCContinueAfterReload(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 6)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	for _, qc := range qcs[:3] {
		require.NoError(t, gp.PersistQC(qc))
	}
	require.NoError(t, gp.Close())

	gp2, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	require.Equal(t, 3, len(gp2.ConsumeLoaded()))
	for _, qc := range qcs[3:] {
		require.NoError(t, gp2.PersistQC(qc))
	}
	require.Equal(t, qcs[5].QC().GlobalRange().Next, gp2.Next())
	require.NoError(t, gp2.Close())

	gp3, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
	require.Equal(t, 6, len(gp3.ConsumeLoaded()))
	require.NoError(t, gp3.Close())
}

func TestFullCommitQCTruncateMidRange(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	qcs := makeSequentialFullCommitQCs(rng, registry, keys, 5)

	gp, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
	require.NoError(t, err)
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

		gp2, err := NewFullCommitQCPersister(utils.Some(dir), registry.FirstBlock())
		require.NoError(t, err)
		require.Equal(t, 5, len(gp2.ConsumeLoaded()))
		require.NoError(t, gp2.Close())
	} else {
		require.NoError(t, gp.Close())
	}
}
