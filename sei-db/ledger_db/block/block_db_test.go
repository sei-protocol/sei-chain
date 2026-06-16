package block_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// open opens a handle to a block.BlockDB. Calling it more than once reopens a
// handle to the SAME backing store, simulating a process restart (in-memory
// impls return the same instance; durable impls reopen their files). The caller
// must Close the previous handle before reopening.
type open func() (block.BlockDB, error)

// builder returns an open bound to a fresh, empty backing store, for one subtest.
type builder func(t *testing.T) open

// TestBlockDB exercises the block.BlockDB contract against every implementation,
// building each via its public constructor. Reclamation-below-watermark is
// impl-specific (see TestLittblockReclaimsAcrossRestart and
// TestMemblockPruneRemovesBelowWatermark); these tests only assert the portable
// safety guarantee (nothing at/above the watermark is removed).
func TestBlockDB(t *testing.T) {
	impls := []struct {
		name  string
		build builder
	}{
		{"memblock", func(t *testing.T) open {
			// One shared instance: reopening returns it, so an in-memory
			// "restart" preserves data exactly as a durable reopen would.
			db := memblock.NewBlockDB()
			return func() (block.BlockDB, error) { return db, nil }
		}},
		{"littblock", func(t *testing.T) open {
			// One backing directory: each open reopens a fresh DB over the same
			// files, so a "restart" actually reloads persisted state from disk.
			dir := t.TempDir()
			return func() (block.BlockDB, error) {
				return littblock.NewBlockDB(littConfig(t, dir))
			}
		}},
	}

	for _, impl := range impls {
		t.Run(impl.name, func(t *testing.T) {
			t.Run("EmptyDB", func(t *testing.T) { testEmptyDB(t, impl.build) })
			t.Run("ReadRoundTrip", func(t *testing.T) { testReadRoundTrip(t, impl.build) })
			t.Run("QCByBlockNumber", func(t *testing.T) { testQCByBlockNumber(t, impl.build) })
			t.Run("Iterators", func(t *testing.T) { testIterators(t, impl.build) })
			t.Run("RestartPersistsData", func(t *testing.T) { testRestartPersistsData(t, impl.build) })
			t.Run("PruneRetainsAtOrAbove", func(t *testing.T) { testPruneRetainsAtOrAbove(t, impl.build) })
			t.Run("WriteOrderRejected", func(t *testing.T) { testWriteOrderRejected(t, impl.build) })
		})
	}
}

// openFresh opens a handle to a new, empty backing store and returns it along
// with the open that can reopen the same store (for restart).
func openFresh(t *testing.T, build builder) (block.BlockDB, open) {
	o := build(t)
	db, err := o()
	require.NoError(t, err)
	return db, o
}

// restart flushes and closes db, then reopens a handle to the same backing
// store. The returned handle must be closed by the caller.
func restart(t *testing.T, o open, db block.BlockDB) block.BlockDB {
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	reopened, err := o()
	require.NoError(t, err)
	return reopened
}

func testEmptyDB(t *testing.T, build builder) {
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	blk, err := db.ReadBlockByNumber(0)
	require.NoError(t, err)
	require.False(t, blk.IsPresent())

	byHash, err := db.ReadBlockByHash(types.GenBlockHeaderHash(utils.TestRngFromSeed(1)))
	require.NoError(t, err)
	require.False(t, byHash.IsPresent())

	qc, err := db.ReadQCByBlockNumber(0)
	require.NoError(t, err)
	require.False(t, qc.IsPresent())

	blockIt, err := db.Blocks()
	require.NoError(t, err)
	ok, err := blockIt.Next()
	require.NoError(t, err)
	require.False(t, ok, "empty db should yield no blocks")
	require.NoError(t, blockIt.Close())

	qcIt, err := db.QCs()
	require.NoError(t, err)
	ok, err = qcIt.Next()
	require.NoError(t, err)
	require.False(t, ok, "empty db should yield no QCs")
	require.NoError(t, qcIt.Close())
}

func testReadRoundTrip(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	assertBlocksReadable(t, db, batches)

	// Misses.
	missNum, err := db.ReadBlockByNumber(1 << 40)
	require.NoError(t, err)
	require.False(t, missNum.IsPresent())

	missHash, err := db.ReadBlockByHash(types.GenBlockHeaderHash(utils.TestRngFromSeed(1)))
	require.NoError(t, err)
	require.False(t, missHash.IsPresent())
}

func testQCByBlockNumber(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	assertQCsReadable(t, db, committee, batches)

	last := batches[len(batches)-1]
	miss, err := db.ReadQCByBlockNumber(last.next + 1000)
	require.NoError(t, err)
	require.False(t, miss.IsPresent())
}

func testIterators(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	assertIterators(t, db, committee, batches)
}

// testRestartPersistsData writes a dataset, restarts (close + reopen the same
// backing store), and asserts every read path and iterator still returns the
// full dataset.
func testRestartPersistsData(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, o := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	db = restart(t, o, db)

	assertBlocksReadable(t, db, batches)
	assertQCsReadable(t, db, committee, batches)
	assertIterators(t, db, committee, batches)
}

// testPruneRetainsAtOrAbove asserts the safety direction of PruneBefore: nothing
// at or above the watermark is removed.
func testPruneRetainsAtOrAbove(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()
	writeAll(t, db, batches)

	// Prune at the start of the second batch.
	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	for _, b := range batches {
		for i, blk := range b.blocks {
			n := b.first + gbn(i)
			if n < watermark {
				continue
			}
			opt, err := db.ReadBlockByNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "block %d (>= watermark %d) must be retained", n, watermark)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())
		}
		if b.next > watermark {
			lookup := b.first
			if lookup < watermark {
				lookup = watermark
			}
			opt, err := db.ReadQCByBlockNumber(lookup)
			require.NoError(t, err)
			require.True(t, opt.IsPresent(), "QC [%d,%d) (Next > watermark) must be retained", b.first, b.next)
		}
	}
}

func testWriteOrderRejected(t *testing.T, build builder) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db, _ := openFresh(t, build)
	defer func() { _ = db.Close() }()

	// Write the first batch normally.
	b0 := batches[0]
	for i, blk := range b0.blocks {
		require.NoError(t, db.WriteBlock(b0.first+gbn(i), blk))
	}
	require.NoError(t, db.WriteQC(b0.first, b0.next, b0.qc))

	// Re-writing an already-written block number is rejected (not idempotent).
	err := db.WriteBlock(b0.first, b0.blocks[0])
	require.ErrorIs(t, err, block.ErrBlockOutOfOrder)

	// Re-writing the same QC (non-contiguous lowerBound) is rejected.
	err = db.WriteQC(b0.first, b0.next, b0.qc)
	require.ErrorIs(t, err, block.ErrQCNonContiguous)

	// The original records are intact after the rejected writes.
	opt, err := db.ReadBlockByNumber(b0.first)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}

// TestMemblockPruneRemovesBelowWatermark verifies the in-memory store's
// synchronous, exact pruning: everything below the watermark is gone
// immediately. Impl-specific (durable stores prune asynchronously) but uses only
// the public API.
func TestMemblockPruneRemovesBelowWatermark(t *testing.T) {
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)
	db := memblock.NewBlockDB()
	writeAll(t, db, batches)

	watermark := batches[1].first
	require.NoError(t, db.PruneBefore(watermark))

	// First batch (below watermark) is gone.
	for i := range batches[0].blocks {
		n := batches[0].first + gbn(i)
		opt, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d should be pruned", n)
	}
	qc, err := db.ReadQCByBlockNumber(batches[0].first)
	require.NoError(t, err)
	require.False(t, qc.IsPresent(), "QC below watermark should be pruned")

	// Watermark block is retained.
	opt, err := db.ReadBlockByNumber(watermark)
	require.NoError(t, err)
	require.True(t, opt.IsPresent())
}

// TestLittblockReclaimsAcrossRestart verifies the durable reclamation path: data
// written, then pruned past after a restart (which seals the segments it landed
// in), is collected by GC. The active segment of a running DB only holds the
// newest data, which is never below the watermark — hence the restart.
func TestLittblockReclaimsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)

	db, err := littblock.NewBlockDB(littConfig(t, dir))
	require.NoError(t, err)
	writeAll(t, db, batches)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen: the segments written above are now sealed and collectable.
	db2, err := littblock.NewBlockDB(littConfig(t, dir))
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	beyond := batches[len(batches)-1].next
	require.NoError(t, db2.PruneBefore(beyond))
	require.NoError(t, littblock.ForceGC(db2))

	for _, b := range batches {
		opt, err := db2.ReadBlockByNumber(b.first)
		require.NoError(t, err)
		require.False(t, opt.IsPresent(), "block %d should be reclaimed after restart", b.first)
		qc, err := db2.ReadQCByBlockNumber(b.first)
		require.NoError(t, err)
		require.False(t, qc.IsPresent(), "QC at %d should be reclaimed after restart", b.first)
	}
}

// littConfig builds a littblock config rooted at dir with a tiny retention so
// the prune watermark is the sole observable reclamation gate in tests.
func littConfig(t *testing.T, dir string) *littblock.LittBlockConfig {
	cfg, err := littblock.DefaultConfig(dir)
	require.NoError(t, err)
	cfg.Retention = time.Nanosecond
	return cfg
}

// --- shared assertions ---

func assertBlocksReadable(t *testing.T, db block.BlockDB, batches []batch) {
	for _, b := range batches {
		for i, blk := range b.blocks {
			n := b.first + gbn(i)

			byNum, err := db.ReadBlockByNumber(n)
			require.NoError(t, err)
			got, ok := byNum.Get()
			require.True(t, ok, "block %d should exist", n)
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())

			byHash, err := db.ReadBlockByHash(blk.Header().Hash())
			require.NoError(t, err)
			got, ok = byHash.Get()
			require.True(t, ok, "block by hash should exist")
			require.Equal(t, blk.Header().Hash(), got.Header().Hash())
		}
	}
}

func assertQCsReadable(t *testing.T, db block.BlockDB, committee *types.Committee, batches []batch) {
	for _, b := range batches {
		r := b.qc.QC().GlobalRange(committee)
		for n := r.First; n < r.Next; n++ {
			opt, err := db.ReadQCByBlockNumber(n)
			require.NoError(t, err)
			got, ok := opt.Get()
			require.True(t, ok, "QC covering %d should exist", n)
			require.Equal(t, r.First, got.QC().GlobalRange(committee).First)
		}
	}
}

func assertIterators(t *testing.T, db block.BlockDB, committee *types.Committee, batches []batch) {
	totalBlocks := 0
	for _, b := range batches {
		totalBlocks += len(b.blocks)
	}

	blockIt, err := db.Blocks()
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

	qcIt, err := db.QCs()
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

// --- block/QC generation (mirrors data.TestCommitQC, which is not importable
// from sei-db because it lives in an internal package) ---

const (
	committeeSize = 4
	blocksPerQC   = 5
	numBatches    = 4
	testSeed      = 20260615
)

var genesisTime = time.Unix(1_700_000_000, 0)

// batch is a contiguous run of blocks at global numbers [first, next) together
// with the QC that finalizes them. next == first+len(blocks).
type batch struct {
	first  types.GlobalBlockNumber
	next   types.GlobalBlockNumber
	blocks []*types.Block
	qc     *types.FullCommitQC
}

// gbn converts a non-negative slice index to a GlobalBlockNumber offset.
func gbn(i int) types.GlobalBlockNumber {
	return types.GlobalBlockNumber(i) //nolint:gosec // i is a non-negative slice index
}

// writeAll writes every batch's blocks (at first+i) followed by its QC.
func writeAll(t *testing.T, db block.BlockDB, batches []batch) {
	for _, b := range batches {
		for i, blk := range b.blocks {
			require.NoError(t, db.WriteBlock(b.first+gbn(i), blk))
		}
		require.NoError(t, db.WriteQC(b.first, b.next, b.qc))
	}
}

// buildCommittee returns a deterministic round-robin committee (global numbering
// from 0) and the secret keys that sign its QCs.
func buildCommittee() (*types.Committee, []types.SecretKey) {
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

// generateBatches builds a deterministic sequence of contiguous finalized
// batches for the given committee/keys.
func generateBatches(committee *types.Committee, keys []types.SecretKey) []batch {
	rng := utils.TestRngFromSeed(testSeed + 1)
	prev := utils.None[*types.CommitQC]()
	batches := make([]batch, 0, numBatches)
	for range numBatches {
		fqc, blocks := buildFullCommitQC(rng, committee, keys, prev)
		r := fqc.QC().GlobalRange(committee)
		batches = append(batches, batch{first: r.First, next: r.Next, blocks: blocks, qc: fqc})
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
