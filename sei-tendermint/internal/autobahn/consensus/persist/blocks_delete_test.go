package persist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// After DeleteLane, a persist attempt under a committee that no longer includes
// the lane must not recreate the WAL directory. Also covers DeleteLane idempotency.
func TestMaybePruneAndPersistLane_InactiveDoesNotRecreateAfterDelete(t *testing.T) {
	rng := utils.TestRng()
	dir := t.TempDir()
	bp, _, err := NewBlockPersister(utils.Some(dir))
	require.NoError(t, err)
	t.Cleanup(func() { _ = bp.Close() })

	leaver := types.GenSecretKey(rng)
	stayer := types.GenSecretKey(rng)
	lane := types.NewLaneID(leaver.Public(), 0)
	proposal := types.Sign(leaver, types.NewLaneProposal(
		types.NewBlock(lane, 0, types.BlockHeaderHash{}, types.GenPayload(rng)),
	))

	require.NoError(t, bp.MaybePruneAndPersistLane(
		lane,
		committeeForLane(lane),
		utils.None[*types.CommitQC](),
		[]*types.Signed[*types.LaneProposal]{proposal},
		noBlockCB,
	))
	lanePath := filepath.Join(dir, blocksDir, laneDir(lane))
	require.NoError(t, bp.DeleteLane(lane))
	_, err = os.Stat(lanePath)
	require.True(t, os.IsNotExist(err))
	require.NoError(t, bp.DeleteLane(lane)) // idempotent

	active := utils.OrPanic1(types.NewCommittee(map[types.PublicKey]uint64{stayer.Public(): 1}))
	require.False(t, active.HasLane(lane))
	require.NoError(t, bp.MaybePruneAndPersistLane(
		lane,
		active,
		utils.None[*types.CommitQC](),
		[]*types.Signed[*types.LaneProposal]{proposal},
		noBlockCB,
	))
	_, err = os.Stat(lanePath)
	require.True(t, os.IsNotExist(err))
}
