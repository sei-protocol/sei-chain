package persist

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func testSignedProposal(rng utils.Rng, key types.SecretKey, n types.BlockNumber) *types.Signed[*types.LaneProposal] {
	lane := key.Public()
	block := types.NewBlock(lane, n, types.GenBlockHeaderHash(rng), types.GenPayload(rng))
	return types.Sign(key, types.NewLaneProposal(block))
}

func TestNewBlockPersisterEmptyDir(t *testing.T) {
	dir := t.TempDir()
	bp, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.NotNil(t, bp)
	require.Equal(t, 0, len(blocks))
	// blocks/ subdirectory should exist
	fi, err := os.Stat(filepath.Join(dir, "blocks"))
	require.NoError(t, err)
	require.True(t, fi.IsDir())
}

func TestPersistBlockAndLoad(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	b0 := testSignedProposal(rng, key, 0)
	b1 := testSignedProposal(rng, key, 1)
	require.NoError(t, bp.PersistBlock(lane, 0, b0))
	require.NoError(t, bp.PersistBlock(lane, 1, b1))

	// Reload from disk
	bp2, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.NotNil(t, bp2)
	require.Equal(t, 1, len(blocks), "should have 1 lane")
	require.Equal(t, 2, len(blocks[lane]), "should have 2 blocks")
	require.NoError(t, utils.TestDiff(b0, blocks[lane][0]))
	require.NoError(t, utils.TestDiff(b1, blocks[lane][1]))
}

func TestPersistBlockMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key1 := types.GenSecretKey(rng)
	key2 := types.GenSecretKey(rng)
	lane1 := key1.Public()
	lane2 := key2.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	b1 := testSignedProposal(rng, key1, 0)
	b2 := testSignedProposal(rng, key2, 0)
	require.NoError(t, bp.PersistBlock(lane1, 0, b1))
	require.NoError(t, bp.PersistBlock(lane2, 0, b2))

	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 2, len(blocks), "should have 2 lanes")
	require.NoError(t, utils.TestDiff(b1, blocks[lane1][0]))
	require.NoError(t, utils.TestDiff(b2, blocks[lane2][0]))
}

func TestLoadSkipsCorruptBlockFile(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	// Write a good block
	b0 := testSignedProposal(rng, key, 0)
	require.NoError(t, bp.PersistBlock(lane, 0, b0))

	// Write a corrupt file with a valid filename
	corruptName := blockFilename(lane, 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blocks", corruptName), []byte("corrupt"), 0600))

	// Reload — should load b0 and skip the corrupt one
	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane]), "should only load the valid block")
	require.NoError(t, utils.TestDiff(b0, blocks[lane][0]))
}

func TestLoadSkipsMismatchedHeader(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key1 := types.GenSecretKey(rng)
	key2 := types.GenSecretKey(rng)
	lane1 := key1.Public()
	lane2 := key2.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	// Write block for lane1 but save it under lane2's filename
	b := testSignedProposal(rng, key1, 5)
	require.NoError(t, bp.PersistBlock(lane1, 5, b))

	// Rename the file to use lane2 in the filename
	oldPath := filepath.Join(dir, "blocks", blockFilename(lane1, 5))
	newPath := filepath.Join(dir, "blocks", blockFilename(lane2, 5))
	require.NoError(t, os.Rename(oldPath, newPath))

	// Reload — should skip the mismatched file
	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 0, len(blocks), "mismatched header should be skipped")
}

func TestLoadSkipsUnrecognizedFilename(t *testing.T) {
	dir := t.TempDir()

	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)
	_ = bp

	// Write files with bad names
	blocksDir := filepath.Join(dir, "blocks")
	require.NoError(t, os.WriteFile(filepath.Join(blocksDir, "notablock.pb"), []byte("data"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(blocksDir, "readme.txt"), []byte("hi"), 0600))

	// Reload — should skip both
	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 0, len(blocks))
}

func TestDeleteBeforeRemovesOldKeepsNew(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	// Persist blocks 0..4
	for i := types.BlockNumber(0); i < 5; i++ {
		require.NoError(t, bp.PersistBlock(lane, i, testSignedProposal(rng, key, i)))
	}

	// Delete blocks before 3
	bp.DeleteBefore(map[types.LaneID]types.BlockNumber{lane: 3})

	// Reload and verify only 3, 4 remain
	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 2, len(blocks[lane]), "should have blocks 3 and 4")
	_, has0 := blocks[lane][0]
	_, has1 := blocks[lane][1]
	_, has2 := blocks[lane][2]
	_, has3 := blocks[lane][3]
	_, has4 := blocks[lane][4]
	require.False(t, has0)
	require.False(t, has1)
	require.False(t, has2)
	require.True(t, has3)
	require.True(t, has4)
}

func TestDeleteBeforeMultipleLanes(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key1 := types.GenSecretKey(rng)
	key2 := types.GenSecretKey(rng)
	lane1 := key1.Public()
	lane2 := key2.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	// Lane1: blocks 0,1,2; Lane2: blocks 0,1,2
	for i := types.BlockNumber(0); i < 3; i++ {
		require.NoError(t, bp.PersistBlock(lane1, i, testSignedProposal(rng, key1, i)))
		require.NoError(t, bp.PersistBlock(lane2, i, testSignedProposal(rng, key2, i)))
	}

	// Delete lane1 < 2, lane2 < 1
	bp.DeleteBefore(map[types.LaneID]types.BlockNumber{lane1: 2, lane2: 1})

	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane1]), "lane1 should have block 2")
	require.Equal(t, 2, len(blocks[lane2]), "lane2 should have blocks 1,2")
}

func TestDeleteBeforeEmptyMap(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()

	key := types.GenSecretKey(rng)
	lane := key.Public()
	bp, _, err := NewBlockPersister(dir)
	require.NoError(t, err)

	require.NoError(t, bp.PersistBlock(lane, 0, testSignedProposal(rng, key, 0)))

	// Empty map — should not delete anything
	bp.DeleteBefore(map[types.LaneID]types.BlockNumber{})

	_, blocks, err := NewBlockPersister(dir)
	require.NoError(t, err)
	require.Equal(t, 1, len(blocks[lane]))
}

func TestBlockFilenameRoundTrip(t *testing.T) {
	rng := utils.TestRng()
	lane := types.GenSecretKey(rng).Public()
	n := types.BlockNumber(42)

	name := blockFilename(lane, n)
	parsedLane, parsedN, err := parseBlockFilename(name)
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(lane.Bytes()), hex.EncodeToString(parsedLane.Bytes()))
	require.Equal(t, n, parsedN)
}
