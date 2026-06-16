// Package blocktest provides a shared conformance suite that any block.BlockDB
// implementation can be run against, so all implementations are held to the
// same contract. It also exports the block/QC generators the suite uses, so
// implementation-specific tests (e.g. reclamation behavior) can reuse them.
package blocktest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const (
	committeeSize = 4
	blocksPerQC   = 5
	numBatches    = 4
	testSeed      = 20260615
)

var genesisTime = time.Unix(1_700_000_000, 0)

// Factory builds a fresh block.BlockDB for one subtest, using the supplied
// committee (so the suite and the implementation agree on QC GlobalRanges). It
// returns the DB and a settle func that forces durability + reclamation (for a
// durable impl: Flush then force GC; for an in-memory impl: a no-op). The
// factory must register its own cleanup via t.Cleanup.
type Factory func(t *testing.T, committee *types.Committee) (db block.BlockDB, settle func() error)

// RunConformance runs the portable block.BlockDB conformance suite against the
// implementation produced by factory. Reclamation-below-watermark is not
// covered here because it is impl-specific (synchronous and exact for an
// in-memory store, asynchronous and segment-granular for a durable one); only
// the portable safety guarantee (nothing at/above the watermark is removed) is
// asserted. Implementations should add their own reclamation tests.
func RunConformance(t *testing.T, factory Factory) {
	t.Run("ReadRoundTrip", func(t *testing.T) { testReadRoundTrip(t, factory) })
	t.Run("QCByBlockNumber", func(t *testing.T) { testQCByBlockNumber(t, factory) })
	t.Run("Iterators", func(t *testing.T) { testIterators(t, factory) })
	t.Run("PruneRetainsAtOrAbove", func(t *testing.T) { testPruneRetainsAtOrAbove(t, factory) })
	t.Run("WriteOrderRejected", func(t *testing.T) { testWriteOrderRejected(t, factory) })
}

func testReadRoundTrip(t *testing.T, factory Factory) {
	ctx := context.Background()
	committee, keys := BuildCommittee()
	db, settle := factory(t, committee)
	batches := GenerateBatches(committee, keys)
	WriteAll(t, db, batches)
	require.NoError(t, settle())

	for _, b := range batches {
		for i, blk := range b.Blocks {
			n := b.First + gbn(i)

			byNum, err := db.ReadBlockByNumber(ctx, n)
			require.NoError(t, err)
			got, ok := byNum.Get()
			require.True(t, ok, "block %d should exist", n)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())

			byHash, err := db.ReadBlockByHash(ctx, blk.Header().Hash())
			require.NoError(t, err)
			got, ok = byHash.Get()
			require.True(t, ok, "block by hash should exist")
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())
		}
	}

	// Misses.
	missNum, err := db.ReadBlockByNumber(ctx, 1<<40)
	require.NoError(t, err)
	require.False(t, missNum.IsPresent())

	missHash, err := db.ReadBlockByHash(ctx, types.GenBlockHeaderHash(utils.TestRngFromSeed(1)))
	require.NoError(t, err)
	require.False(t, missHash.IsPresent())
}

func testQCByBlockNumber(t *testing.T, factory Factory) {
	ctx := context.Background()
	committee, keys := BuildCommittee()
	db, settle := factory(t, committee)
	batches := GenerateBatches(committee, keys)
	WriteAll(t, db, batches)
	require.NoError(t, settle())

	var maxNext types.GlobalBlockNumber
	for _, b := range batches {
		r := b.QC.QC().GlobalRange(committee)
		maxNext = r.Next
		for n := r.First; n < r.Next; n++ {
			opt, err := db.ReadQCByBlockNumber(ctx, n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "QC covering %d should exist", n)
			require.Equal(t, r.First, got.QC().GlobalRange(committee).First)
		}
	}

	miss, err := db.ReadQCByBlockNumber(ctx, maxNext+1000)
	require.NoError(t, err)
	require.False(t, miss.IsPresent())
}

func testIterators(t *testing.T, factory Factory) {
	ctx := context.Background()
	committee, keys := BuildCommittee()
	db, settle := factory(t, committee)
	batches := GenerateBatches(committee, keys)
	WriteAll(t, db, batches)
	require.NoError(t, settle())

	totalBlocks := 0
	for _, b := range batches {
		totalBlocks += len(b.Blocks)
	}

	blockIt, err := db.Blocks(ctx)
	require.NoError(t, err)
	count := 0
	var prev types.GlobalBlockNumber
	havePrev := false
	for {
		ok, err := blockIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		n := blockIt.Number()
		if havePrev {
			require.Greater(t, n, prev, "blocks must iterate ascending")
		}
		prev, havePrev = n, true
		blk, err := blockIt.Block()
		require.NoError(t, err)
		require.NotNil(t, blk)
		count++
	}
	require.NoError(t, blockIt.Close())
	require.Equal(t, totalBlocks, count)

	qcIt, err := db.QCs(ctx)
	require.NoError(t, err)
	qcCount := 0
	var prevFirst types.GlobalBlockNumber
	haveQC := false
	for {
		ok, err := qcIt.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		qc, err := qcIt.QC()
		require.NoError(t, err)
		first := qc.QC().GlobalRange(committee).First
		if haveQC {
			require.Greater(t, first, prevFirst, "QCs must iterate ascending by First")
		}
		prevFirst, haveQC = first, true
		qcCount++
	}
	require.NoError(t, qcIt.Close())
	require.Equal(t, len(batches), qcCount)
}

// testPruneRetainsAtOrAbove asserts the safety direction of PruneBefore: nothing
// at or above the watermark is removed.
func testPruneRetainsAtOrAbove(t *testing.T, factory Factory) {
	ctx := context.Background()
	committee, keys := BuildCommittee()
	db, settle := factory(t, committee)
	batches := GenerateBatches(committee, keys)
	WriteAll(t, db, batches)
	require.NoError(t, settle())

	// Prune at the start of the second batch.
	watermark := batches[1].First
	require.NoError(t, db.PruneBefore(ctx, watermark))
	require.NoError(t, settle())

	for _, b := range batches {
		r := b.QC.QC().GlobalRange(committee)
		for i, blk := range b.Blocks {
			n := b.First + gbn(i)
			if n < watermark {
				continue
			}
			opt, err := db.ReadBlockByNumber(ctx, n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "block %d (>= watermark %d) must be retained", n, watermark)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())
		}
		if r.Next > watermark {
			lookup := r.First
			if lookup < watermark {
				lookup = watermark
			}
			opt, err := db.ReadQCByBlockNumber(ctx, lookup)
			require.NoError(t, err)
			require.True(t, opt.IsPresent(), "QC [%d,%d) (Next > watermark) must be retained", r.First, r.Next)
		}
	}
}

func testWriteOrderRejected(t *testing.T, factory Factory) {
	ctx := context.Background()
	committee, keys := BuildCommittee()
	db, settle := factory(t, committee)
	batches := GenerateBatches(committee, keys)

	// Write the first batch normally.
	b0 := batches[0]
	for i, blk := range b0.Blocks {
		require.NoError(t, db.WriteBlock(ctx, b0.First+gbn(i), blk))
	}
	require.NoError(t, db.WriteQC(ctx, b0.QC))

	// Re-writing an already-written block number is rejected (not idempotent).
	err := db.WriteBlock(ctx, b0.First, b0.Blocks[0])
	require.ErrorIs(t, err, block.ErrBlockOutOfOrder)

	// Re-writing the same QC (non-contiguous First) is rejected.
	err = db.WriteQC(ctx, b0.QC)
	require.ErrorIs(t, err, block.ErrQCNonContiguous)

	require.NoError(t, settle())

	// The original records are intact after the rejected writes.
	opt, err := db.ReadBlockByNumber(ctx, b0.First)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}

// gbn converts a non-negative slice index to a GlobalBlockNumber offset.
func gbn(i int) types.GlobalBlockNumber {
	return types.GlobalBlockNumber(i) //nolint:gosec // i is a non-negative slice index
}

// WriteAll writes every batch's blocks (at first+i) followed by its QC.
func WriteAll(t *testing.T, db block.BlockDB, batches []Batch) {
	ctx := context.Background()
	for _, b := range batches {
		for i, blk := range b.Blocks {
			require.NoError(t, db.WriteBlock(ctx, b.First+gbn(i), blk))
		}
		require.NoError(t, db.WriteQC(ctx, b.QC))
	}
}

// --- block/QC generation (mirrors data.TestCommitQC, which is not importable
// from sei-db because it lives in an internal package) ---

// Batch is a contiguous run of blocks at global numbers [First, First+len(Blocks))
// together with the QC that finalizes them.
type Batch struct {
	First  types.GlobalBlockNumber
	Blocks []*types.Block
	QC     *types.FullCommitQC
}

// BuildCommittee returns a deterministic round-robin committee (global numbering
// from 0) and the secret keys that sign its QCs.
func BuildCommittee() (*types.Committee, []types.SecretKey) {
	rng := utils.TestRngFromSeed(testSeed)
	keys := make([]types.SecretKey, committeeSize)
	replicas := make([]types.PublicKey, committeeSize)
	for i := range keys {
		keys[i] = types.GenSecretKey(rng)
		replicas[i] = keys[i].Public()
	}
	committee := utils.OrPanic1(types.NewRoundRobinElection(replicas, 0, genesisTime))
	return committee, keys
}

// GenerateBatches builds a deterministic sequence of contiguous finalized
// batches for the given committee/keys.
func GenerateBatches(committee *types.Committee, keys []types.SecretKey) []Batch {
	rng := utils.TestRngFromSeed(testSeed + 1)
	prev := utils.None[*types.CommitQC]()
	batches := make([]Batch, 0, numBatches)
	for range numBatches {
		fqc, blocks := buildFullCommitQC(rng, committee, keys, prev)
		first := fqc.QC().GlobalRange(committee).First
		batches = append(batches, Batch{First: first, Blocks: blocks, QC: fqc})
		prev = utils.Some(fqc.QC())
	}
	return batches
}

func buildFullCommitQC(
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
) (*types.FullCommitQC, []*types.Block) {
	blocks := map[types.LaneID][]*types.Block{}
	makeBlock := func(producer types.LaneID) *types.Block {
		if bs := blocks[producer]; len(bs) > 0 {
			parent := bs[len(bs)-1]
			return types.NewBlock(producer, parent.Header().Next(), parent.Header().Hash(), types.GenPayload(rng))
		}
		return types.NewBlock(
			producer,
			types.LaneRangeOpt(prev, producer).Next(),
			types.GenBlockHeaderHash(rng),
			types.GenPayload(rng),
		)
	}
	for range blocksPerQC {
		producer := committee.Lanes().At(rng.Intn(committee.Lanes().Len()))
		blocks[producer] = append(blocks[producer], makeBlock(producer))
	}

	laneQCs := map[types.LaneID]*types.LaneQC{}
	var headers []*types.BlockHeader
	var blockList []*types.Block
	for lane := range committee.Lanes().All() {
		if bs := blocks[lane]; len(bs) > 0 {
			laneQCs[lane] = testLaneQC(keys, bs[len(bs)-1].Header())
			for _, b := range bs {
				headers = append(headers, b.Header())
				blockList = append(blockList, b)
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
		genesisTime,
		laneQCs,
		func() utils.Option[*types.AppQC] {
			if n := types.GlobalRangeOpt(prev, committee).Next; n > 0 {
				p := types.NewAppProposal(n-1, viewSpec.View().Index, types.GenAppHash(rng))
				return utils.Some(testAppQC(keys, p))
			}
			return utils.None[*types.AppQC]()
		}(),
	))
	votes := make([]*types.Signed[*types.CommitVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, types.NewCommitVote(proposal.Proposal().Msg())))
	}
	return types.NewFullCommitQC(types.NewCommitQC(votes), headers), blockList
}

func testLaneQC(keys []types.SecretKey, header *types.BlockHeader) *types.LaneQC {
	vote := types.NewLaneVote(header)
	votes := make([]*types.Signed[*types.LaneVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewLaneQC(votes)
}

func testAppQC(keys []types.SecretKey, proposal *types.AppProposal) *types.AppQC {
	vote := types.NewAppVote(proposal)
	votes := make([]*types.Signed[*types.AppVote], 0, len(keys))
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewAppQC(votes)
}
