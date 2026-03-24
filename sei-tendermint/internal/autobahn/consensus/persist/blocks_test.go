package persist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
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

func testPersistBlock(t *testing.T, bp *BlockPersister, p *types.Signed[*types.LaneProposal]) {
	t.Helper()
	require.NoError(t, bp.MaybePruneAndPersistLane(
		p.Msg().Block().Header().Lane(),
		utils.None[*types.CommitQC](),
		[]*types.Signed[*types.LaneProposal]{p},
		noBlockCB,
	))
}

// testDeleteBefore is a test helper that truncates lane WALs using a plain
// map, avoiding the need to construct a full CommitQC.
func testDeleteBefore(bp *BlockPersister, laneFirsts map[types.LaneID]types.BlockNumber) error {
	for lanes := range bp.lanes.RLock() {
		return scope.Parallel(func(ps scope.ParallelScope) error {
			for lane, first := range laneFirsts {
				lw, ok := lanes[lane]
				if !ok {
					continue
				}
				ps.Spawn(func() error {
					for s := range lw.state.Lock() {
						return s.truncateForAnchor(lane, first)
					}
					panic("unreachable")
				})
			}
			return nil
		})
	}
	panic("unreachable")
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
	require.NoError(t, bp.close())
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
	require.NoError(t, bp.close())

	bp2, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.NotNil(t, bp2)
	require.Equal(t, 1, len(blocks), "should have 1 lane")
	require.Equal(t, 2, len(blocks[lane]), "should have 2 blocks")
	require.Equal(t, types.BlockNumber(0), blocks[lane][0].Number)
	require.Equal(t, types.BlockNumber(1), blocks[lane][1].Number)
	require.NoError(t, utils.TestDiff(b0, blocks[lane][0].Proposal))
	require.NoError(t, utils.TestDiff(b1, blocks[lane][1].Proposal))
	require.NoError(t, bp2.close())
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
	require.NoError(t, bp.close())

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

	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane: 3}))
	require.NoError(t, bp.close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(blocks[lane]), "should have blocks 3 and 4")
	require.Equal(t, types.BlockNumber(3), blocks[lane][0].Number)
	require.Equal(t, types.BlockNumber(4), blocks[lane][1].Number)
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
	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane1: 2, lane2: 0, lane3: 0}))
	require.NoError(t, bp.close())

	// Restart — verify varied lane states load correctly.
	bp2, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane1]), "lane1 should have block 2")
	require.Equal(t, types.BlockNumber(2), blocks[lane1][0].Number)
	require.Equal(t, 3, len(blocks[lane2]), "lane2 should have all 3 blocks")
	require.Equal(t, 0, len(blocks[lane3]), "lane3 never had blocks")

	// Persist more after restart, then restart again to verify continuity.
	testPersistBlock(t, bp2, testSignedProposal(rng, key1, 3))
	testPersistBlock(t, bp2, testSignedProposal(rng, key2, 3))
	require.NoError(t, bp2.close())

	_, blocks2, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 2, len(blocks2[lane1]), "lane1 should have blocks 2,3")
	require.Equal(t, types.BlockNumber(3), blocks2[lane1][1].Number)
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
	testPersistBlock(t, bp, testSignedProposal(rng, key, 0))
	require.NoError(t, bp.MaybePruneAndPersistLane(types.LaneID{}, utils.None[*types.CommitQC](), nil, noBlockCB)) // no-op persister returns early
	require.NoError(t, bp.close())
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
	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane: 3}))
	testPersistBlock(t, bp, testSignedProposal(rng, key, 5))
	require.NoError(t, bp.close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 3, len(blocks[lane]), "should have blocks 3, 4, 5")
	require.Equal(t, types.BlockNumber(3), blocks[lane][0].Number)
	require.Equal(t, types.BlockNumber(4), blocks[lane][1].Number)
	require.Equal(t, types.BlockNumber(5), blocks[lane][2].Number)
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
	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane: 10}))

	// Lane WAL is now empty; new writes starting from 10 should work.
	testPersistBlock(t, bp, testSignedProposal(rng, key, 10))
	testPersistBlock(t, bp, testSignedProposal(rng, key, 11))
	require.NoError(t, bp.close())

	// Reopen — should see only the post-TruncateAll blocks.
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
	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane: 10}))

	// Writing a stale block number (0) should be rejected.
	stale := testSignedProposal(rng, key, 0)
	err = bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{stale}, noBlockCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")

	// Writing at the correct anchor (10) should succeed.
	testPersistBlock(t, bp, testSignedProposal(rng, key, 10))
	require.NoError(t, bp.close())
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
	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane: 10}))

	// Second truncation on the already-empty WAL (first=15).
	// Before the fix, nextBlockNum would stay at 10 and block 15 would
	// be rejected as out of sequence.
	require.NoError(t, testDeleteBefore(bp, map[types.LaneID]types.BlockNumber{lane: 15}))

	testPersistBlock(t, bp, testSignedProposal(rng, key, 15))
	require.NoError(t, bp.close())
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
	require.NoError(t, bp.close())

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
	require.NoError(t, bp.close())
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
	require.NoError(t, bp.close())
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
	err = bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{gap}, noBlockCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")

	// Duplicate: try block 0 again.
	dup := testSignedProposal(rng, key, 0)
	err = bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), []*types.Signed[*types.LaneProposal]{dup}, noBlockCB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of sequence")

	require.NoError(t, bp.close())
}

func TestLoadAllDetectsBlockGap(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	key := types.GenSecretKey(rng)
	lane := key.Public()

	// Write directly to a lane WAL, bypassing the contiguity check
	// to simulate on-disk corruption (block 0 then block 2, skipping 1).
	ld := filepath.Join(dir, blocksDir, laneDir(lane))
	require.NoError(t, os.MkdirAll(ld, 0700))
	s, err := newLaneWALState(ld)
	require.NoError(t, err)
	require.NoError(t, s.Write(testSignedProposal(rng, key, 0)))
	require.NoError(t, s.Write(testSignedProposal(rng, key, 2)))
	require.NoError(t, s.Close())

	_, _, err = NewBlockPersister(utils.Some(dir))
	require.Error(t, err)
	require.Contains(t, err.Error(), "gap")
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

	require.NoError(t, bp.close())

	_, blocks, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane]))
	require.Equal(t, types.BlockNumber(0), blocks[lane][0].Number)
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
				return bp.MaybePruneAndPersistLane(lane, utils.None[*types.CommitQC](), proposals[i], noBlockCB)
			})
		}
		return nil
	}))

	require.NoError(t, bp.close())

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
