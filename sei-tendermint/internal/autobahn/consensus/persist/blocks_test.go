package persist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func testSignedProposal(rng utils.Rng, key types.SecretKey, n types.BlockNumber) *types.Signed[*types.LaneProposal] {
	lane := key.Public()
	block := types.NewBlock(lane, n, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

var noBlockCB = utils.None[func(*types.Signed[*types.LaneProposal])]()

// liveBlocks drops blocks the prune anchor has moved past, mirroring the filter loadPersistedState
// applies in the avail package. Pruning reclaims whole WAL files, so a pruned block can still be on
// disk when the persister reloads; only what the anchor considers live is asserted on here.
func liveBlocks(loaded []LoadedBlock, first types.BlockNumber) []LoadedBlock {
	for i, b := range loaded {
		if b.Number >= first {
			return loaded[i:]
		}
	}
	return nil
}

func testPersistBlock(t *testing.T, bp *BlockPersister, p *types.Signed[*types.LaneProposal]) {
	t.Helper()
	require.NoError(t, bp.Persist(
		p.Msg().Block().Header().Lane(),
		0,
		[]*types.Signed[*types.LaneProposal]{p},
		noBlockCB,
	))
}

func TestNewBlockPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	bp, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, bp)
	require.Equal(t, 0, len(blocks))
	fi, err := os.Stat(filepath.Join(dir, blocksDir))
	require.NoError(t, err)
	require.True(t, fi.IsDir())
	require.NoError(t, bp.Close())
}

func TestPersistBlockAndLoad(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	b0 := testSignedProposal(rng, key, 0)
	b1 := testSignedProposal(rng, key, 1)
	testPersistBlock(t, bp, b0)
	testPersistBlock(t, bp, b1)
	require.NoError(t, bp.Close())

	bp2, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, bp2)
	require.Equal(t, 1, len(blocks), "should have 1 lane")
	require.Equal(t, 2, len(blocks[lane]), "should have 2 blocks")
	require.Equal(t, types.BlockNumber(0), blocks[lane][0].Number)
	require.Equal(t, types.BlockNumber(1), blocks[lane][1].Number)
	require.NoError(t, utils.TestDiff(b0, blocks[lane][0].Proposal))
	require.NoError(t, utils.TestDiff(b1, blocks[lane][1].Proposal))
	require.NoError(t, bp2.Close())
}

func TestPersistBlockMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key1 := types.GenSecretKey(rng)
	key2 := types.GenSecretKey(rng)
	lane1 := key1.Public()
	lane2 := key2.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	b1 := testSignedProposal(rng, key1, 0)
	b2 := testSignedProposal(rng, key2, 0)
	testPersistBlock(t, bp, b1)
	testPersistBlock(t, bp, b2)
	require.NoError(t, bp.Close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(blocks), "should have 2 lanes")
	require.Equal(t, 1, len(blocks[lane1]))
	require.Equal(t, 1, len(blocks[lane2]))
	require.NoError(t, utils.TestDiff(b1, blocks[lane1][0].Proposal))
	require.NoError(t, utils.TestDiff(b2, blocks[lane2][0].Proposal))
}

func TestDeleteBeforeRemovesOldKeepsNew(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	for i := range types.BlockNumber(5) {
		testPersistBlock(t, bp, testSignedProposal(rng, key, i))
	}

	require.NoError(t, bp.Persist(lane, 3, nil, noBlockCB))
	require.NoError(t, bp.close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	live := liveBlocks(blocks[lane], 3)
	require.Equal(t, 2, len(live), "should have blocks 3 and 4")
	require.Equal(t, types.BlockNumber(3), live[0].Number)
	require.Equal(t, types.BlockNumber(4), live[1].Number)
}

func TestDeleteBeforeAndRestart(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key1 := types.GenSecretKey(rng)
	key2 := types.GenSecretKey(rng)
	key3 := types.GenSecretKey(rng)
	lane1 := key1.Public()
	lane2 := key2.Public()
	lane3 := key3.Public() // never persisted — exercises the "no WAL yet" path
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	for i := range types.BlockNumber(3) {
		testPersistBlock(t, bp, testSignedProposal(rng, key1, i))
		testPersistBlock(t, bp, testSignedProposal(rng, key2, i))
	}

	// lane1: truncate old blocks, lane2: delete nothing (first=0), lane3: empty (no WAL).
	require.NoError(t, bp.Persist(lane1, 2, nil, noBlockCB))
	require.NoError(t, bp.Persist(lane2, 0, nil, noBlockCB))
	require.NoError(t, bp.Persist(lane3, 0, nil, noBlockCB))
	require.NoError(t, bp.close())

	// Restart — verify varied lane states load correctly.
	bp2, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	lane1Live := liveBlocks(blocks[lane1], 2)
	require.Equal(t, 1, len(lane1Live), "lane1 should have block 2")
	require.Equal(t, types.BlockNumber(2), lane1Live[0].Number)
	require.Equal(t, 3, len(blocks[lane2]), "lane2 should have all 3 blocks")
	require.Equal(t, 0, len(blocks[lane3]), "lane3 never had blocks")

	// Persist more after restart, then restart again to verify continuity.
	testPersistBlock(t, bp2, testSignedProposal(rng, key1, 3))
	testPersistBlock(t, bp2, testSignedProposal(rng, key2, 3))
	require.NoError(t, bp2.Close())

	_, blocks2, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	lane1Live2 := liveBlocks(blocks2[lane1], 2)
	require.Equal(t, 2, len(lane1Live2), "lane1 should have blocks 2,3")
	require.Equal(t, types.BlockNumber(3), lane1Live2[1].Number)
	require.Equal(t, 4, len(blocks2[lane2]), "lane2 should have blocks 0..3")
	require.Equal(t, types.BlockNumber(3), blocks2[lane2][3].Number)
}

func TestNoOpBlockPersister(t *testing.T) {
	bp, blocks, err := NewBlockPersister(utils.None[string]())
	require.NoError(t, err)
	require.NotNil(t, bp)
	require.Equal(t, 0, len(blocks))

	rng := utils.TestRng()
	key := types.GenSecretKey(rng)
	lane := key.Public()

	proposals := make([]*types.Signed[*types.LaneProposal], 5)
	for i := range proposals {
		proposals[i] = testSignedProposal(rng, key, types.BlockNumber(i))
	}

	// Persist and prune with first + new proposals in no-op mode.
	// Verify afterEach is still invoked for every proposal.
	var called int
	cb := utils.Some(func(_ *types.Signed[*types.LaneProposal]) { called++ })
	require.NoError(t, bp.Persist(lane, 0, proposals[:3], cb))
	require.Equal(t, 3, called)

	called = 0
	require.NoError(t, bp.Persist(lane, 0, proposals[3:], cb))
	require.Equal(t, 2, called)

	require.NoError(t, bp.Close())
}

func TestDeleteBeforeThenPersistMore(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	// Persist 0..4, delete before 3, then persist 5.
	for i := range types.BlockNumber(5) {
		testPersistBlock(t, bp, testSignedProposal(rng, key, i))
	}
	require.NoError(t, bp.Persist(lane, 3, nil, noBlockCB))
	testPersistBlock(t, bp, testSignedProposal(rng, key, 5))
	require.NoError(t, bp.Close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	live := liveBlocks(blocks[lane], 3)
	require.Equal(t, 3, len(live), "should have blocks 3, 4, 5")
	require.Equal(t, types.BlockNumber(3), live[0].Number)
	require.Equal(t, types.BlockNumber(4), live[1].Number)
	require.Equal(t, types.BlockNumber(5), live[2].Number)
}

func TestDeleteBeforePastAllBlocks(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	for i := range types.BlockNumber(3) {
		testPersistBlock(t, bp, testSignedProposal(rng, key, i))
	}

	// Anchor advanced past everything (nextBlockNum is 3, first=10).
	require.NoError(t, bp.Persist(lane, 10, nil, noBlockCB))

	// Lane WAL is now empty; new writes starting from 10 should work.
	testPersistBlock(t, bp, testSignedProposal(rng, key, 10))
	testPersistBlock(t, bp, testSignedProposal(rng, key, 11))
	require.NoError(t, bp.Close())

	// Reopen — the fast-forward left a gap, so only the blocks after it are live.
	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(blocks[lane]))
	require.Equal(t, types.BlockNumber(10), blocks[lane][0].Number)
	require.Equal(t, types.BlockNumber(11), blocks[lane][1].Number)
}

func TestDeleteBeforePastAllRejectsStaleBlock(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	for i := range types.BlockNumber(3) {
		testPersistBlock(t, bp, testSignedProposal(rng, key, i))
	}

	// Anchor advanced past everything; nextBlockNum re-anchored to 10.
	require.NoError(t, bp.Persist(lane, 10, nil, noBlockCB))

	// Writing a stale block number (0) should be rejected.
	stale := testSignedProposal(rng, key, 0)
	err = bp.Persist(lane, 10, []*types.Signed[*types.LaneProposal]{stale}, noBlockCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")

	// Writing at the correct anchor (10) should succeed.
	testPersistBlock(t, bp, testSignedProposal(rng, key, 10))
	require.NoError(t, bp.Close())
}

func TestTruncateOnEmptyWALAdvancesCursor(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	for i := range types.BlockNumber(3) {
		testPersistBlock(t, bp, testSignedProposal(rng, key, i))
	}

	// First truncation empties the WAL (first=10 > nextBlockNum=3).
	require.NoError(t, bp.Persist(lane, 10, nil, noBlockCB))

	// Second truncation on the already-empty WAL (first=15).
	// Before the fix, nextBlockNum would stay at 10 and block 15 would
	// be rejected as out of sequence.
	require.NoError(t, bp.Persist(lane, 15, nil, noBlockCB))

	testPersistBlock(t, bp, testSignedProposal(rng, key, 15))
	require.NoError(t, bp.Close())
}

func TestEmptyLaneWALSurvivesReopen(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()

	// Simulate a crash after lazy lane directory creation but before any write:
	// create the lane subdirectory so NewBlockPersister discovers it on open.
	bd := filepath.Join(dir, blocksDir)
	require.NoError(t, os.MkdirAll(filepath.Join(bd, laneDir(lane)), 0700))

	// Reopen — empty lane WAL should be loaded and usable.
	bp, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 0, len(blocks[lane]), "no blocks loaded")

	// Persist a new block into the lane without needing lazy creation.
	testPersistBlock(t, bp, testSignedProposal(rng, key, 0))
	require.NoError(t, bp.Close())

	// Reopen — should see the new block.
	_, blocks2, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks2[lane]))
	require.Equal(t, types.BlockNumber(0), blocks2[lane][0].Number)
}

func TestNewBlockPersisterSkipsNonHexDir(t *testing.T) {
	dir := t.TempDir()
	bd := filepath.Join(dir, blocksDir)
	require.NoError(t, os.MkdirAll(bd, 0700))

	// Create a non-hex directory and a regular file — both should be skipped.
	require.NoError(t, os.Mkdir(filepath.Join(bd, "not-valid-hex"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(bd, "stray-file.txt"), []byte("hi"), 0600))

	bp, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 0, len(blocks))
	require.NoError(t, bp.Close())
}

func TestNewBlockPersisterSkipsInvalidKeyDir(t *testing.T) {
	dir := t.TempDir()
	bd := filepath.Join(dir, blocksDir)
	require.NoError(t, os.MkdirAll(bd, 0700))

	// Valid hex but too short to be a valid ed25519 public key.
	require.NoError(t, os.Mkdir(filepath.Join(bd, "abcd"), 0700))

	bp, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 0, len(blocks))
	require.NoError(t, bp.Close())
}

func TestPersistBlockOutOfSequence(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	lane := key.Public()
	testPersistBlock(t, bp, testSignedProposal(rng, key, 0))

	// Gap: skip block 1, try block 2.
	gap := testSignedProposal(rng, key, 2)
	err = bp.Persist(lane, 0, []*types.Signed[*types.LaneProposal]{gap}, noBlockCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")

	// Duplicate: try block 0 again.
	dup := testSignedProposal(rng, key, 0)
	err = bp.Persist(lane, 0, []*types.Signed[*types.LaneProposal]{dup}, noBlockCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")

	require.NoError(t, bp.Close())
}

// TestLoadAllDropsBlocksBehindGap verifies that a gap in the stored block numbers is read as a prune
// boundary: everything before it was logically removed and is discarded, and only the contiguous run
// ending at the newest block is loaded.
func TestLoadAllDropsBlocksBehindGap(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	key := types.GenSecretKey(rng)
	lane := key.Public()

	// Write straight to a lane WAL, bypassing the contiguity check, to lay down blocks 0 and 2 with
	// no block 1 between them.
	ld := filepath.Join(dir, blocksDir, laneDir(lane))
	require.NoError(t, os.MkdirAll(ld, 0700))
	s, err := newLaneWALState(ld)
	require.NoError(t, err)
	require.NoError(t, s.wal.Append(0, testSignedProposal(rng, key, 0)))
	require.NoError(t, s.wal.Append(2, testSignedProposal(rng, key, 2)))
	require.NoError(t, s.wal.Close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane]), "block 0 sits behind the gap and is dropped")
	require.Equal(t, types.BlockNumber(2), blocks[lane][0].Number)
}

func TestPersistBlockAutoCreatesLane(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	entries, _ := os.ReadDir(filepath.Join(dir, blocksDir))
	require.Equal(t, 0, len(entries))

	key := types.GenSecretKey(rng)
	lane := key.Public()
	testPersistBlock(t, bp, testSignedProposal(rng, key, 0))

	entries, _ = os.ReadDir(filepath.Join(dir, blocksDir))
	require.Equal(t, 1, len(entries), "should have 1 lane directory")

	require.NoError(t, bp.Close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane]))
	require.Equal(t, types.BlockNumber(0), blocks[lane][0].Number)
}

// TestPruneReclaimsSealedFiles verifies that the block number truncateForAnchor prunes at actually
// reaches the WAL. Given a file size small enough that blocks roll into sealed files, pruning deletes
// the files below the anchor, so the oldest block a reopened lane reports has moved up — while every
// block at or above the anchor survives.
func TestPruneReclaimsSealedFiles(t *testing.T) {
	rng := utils.TestRng()
	key := types.GenSecretKey(rng)
	lane := key.Public()
	dir := t.TempDir()

	const total = 40
	const anchor = types.BlockNumber(30)
	// Small enough that a file seals every block or two, giving pruning whole files to reclaim.
	const fileSize = 512

	w, err := openWAL(dir, blocksWALName, types.SignedLaneProposalConv, fileSize, blocksWALMetrics)
	require.NoError(t, err)
	s := &laneWALState{wal: w}
	for i := range types.BlockNumber(total) {
		require.NoError(t, s.persistBlock(testSignedProposal(rng, key, i)))
	}
	require.NoError(t, s.flush(lane))
	require.NoError(t, s.truncateForAnchor(lane, anchor))
	require.NoError(t, s.wal.Close())

	w2, err := openWAL(dir, blocksWALName, types.SignedLaneProposalConv, fileSize, blocksWALMetrics)
	require.NoError(t, err)
	s2 := &laneWALState{wal: w2}
	loaded, err := s2.loadAll(lane)
	require.NoError(t, err)
	require.NoError(t, s2.wal.Close())

	require.True(t, len(loaded) > 0, "live blocks must survive pruning")
	require.True(t, loaded[0].Number > 0, "pruning should have reclaimed the oldest blocks")
	require.True(t, loaded[0].Number <= anchor, "pruning must not reclaim blocks at or above the anchor")
	require.Equal(t, types.BlockNumber(total-1), loaded[len(loaded)-1].Number)
	require.Equal(t, types.BlockNumber(total), s2.nextBlockNum)
}

// TestPersistBlockInvokesAfterEachOncePerBlock covers the on-disk path: appends are flushed as a batch
// and afterEach then reports every block in it, exactly once and in order.
func TestPersistBlockInvokesAfterEachOncePerBlock(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	proposals := make([]*types.Signed[*types.LaneProposal], 5)
	for i := range proposals {
		proposals[i] = testSignedProposal(rng, key, types.BlockNumber(i))
	}

	var seen []types.BlockNumber
	cb := utils.Some(func(p *types.Signed[*types.LaneProposal]) {
		seen = append(seen, p.Msg().Block().Header().BlockNumber())
	})
	require.NoError(t, bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), proposals, cb))
	require.NoError(t, bp.Close())

	require.Equal(t, len(proposals), len(seen))
	for i := range seen {
		require.Equal(t, types.BlockNumber(i), seen[i])
	}
}

func TestPersistBlockConcurrentDistinctLanes(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	const numLanes = 8
	const blocksPerLane = 20

	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)

	keys := make([]types.SecretKey, numLanes)
	for i := range numLanes {
		keys[i] = types.GenSecretKey(rng)
	}

	// Each lane prepares its proposals up front (rng is not thread-safe).
	proposals := make([][]*types.Signed[*types.LaneProposal], numLanes)
	for i := range numLanes {
		proposals[i] = make([]*types.Signed[*types.LaneProposal], blocksPerLane)
		for j := range blocksPerLane {
			proposals[i][j] = testSignedProposal(rng, keys[i], types.BlockNumber(j))
		}
	}

	require.NoError(t, scope.Parallel(func(ps scope.ParallelScope) error {
		for i := range numLanes {
			lane := keys[i].Public()
			ps.Spawn(func() error {
				return bp.Persist(lane, 0, proposals[i], noBlockCB)
			})
		}
		return nil
	}))

	require.NoError(t, bp.Close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, numLanes, len(blocks))
	for i := range numLanes {
		lane := keys[i].Public()
		require.Equal(t, blocksPerLane, len(blocks[lane]))
		for j := range blocksPerLane {
			require.Equal(t, types.BlockNumber(j), blocks[lane][j].Number)
		}
	}
}
