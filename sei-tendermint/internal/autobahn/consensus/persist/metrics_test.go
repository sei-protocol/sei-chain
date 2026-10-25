package persist

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func recordCount(t *testing.T, wal, stage string) int64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, Global.recordsAt(wal, stage).Write(&m))
	return int64(m.GetCounter().GetValue())
}

func TestPersistRecordCounters(t *testing.T) {
	t.Run("blocks", func(t *testing.T) {
		rng := utils.TestRng()
		dir := t.TempDir()
		key := types.GenSecretKey(rng)
		lane := types.LaneID{Validator: key.Public(), Joined: 0}
		bp, _, err := NewBlockPersister(utils.Some(dir))
		require.NoError(t, err)
		t.Cleanup(func() { _ = bp.Close() })

		asked := recordCount(t, walBlocks, stageAsked)
		persisted := recordCount(t, walBlocks, stagePersisted)

		require.NoError(t, bp.PruneAndPersist(lane, 0, []*types.Signed[*types.LaneProposal]{
			testSignedProposal(rng, key, 0),
			testSignedProposal(rng, key, 1),
		}))
		require.Equal(t, asked+2, recordCount(t, walBlocks, stageAsked))
		require.Equal(t, persisted+2, recordCount(t, walBlocks, stagePersisted))

		err = bp.PruneAndPersist(lane, 0, []*types.Signed[*types.LaneProposal]{testSignedProposal(rng, key, 0)})
		require.Error(t, err)
		require.Equal(t, asked+2, recordCount(t, walBlocks, stageAsked))
		require.Equal(t, persisted+2, recordCount(t, walBlocks, stagePersisted))
	})

	t.Run("commitqcs", func(t *testing.T) {
		rng := utils.TestRng()
		dir := t.TempDir()
		committee, keys := genTestCommittee(rng, 4)
		qcs := makeSequentialCommitQCs(committee, keys, 2)
		cp, _, err := NewCommitQCPersister(utils.Some(dir))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cp.Close() })

		asked := recordCount(t, walCommitQCs, stageAsked)
		persisted := recordCount(t, walCommitQCs, stagePersisted)

		err = cp.PruneAndPersist(0, []*types.CommitQC{qcs[1]})
		require.Error(t, err)
		require.Equal(t, asked, recordCount(t, walCommitQCs, stageAsked))
		require.Equal(t, persisted, recordCount(t, walCommitQCs, stagePersisted))

		require.NoError(t, cp.PruneAndPersist(0, qcs))
		require.Equal(t, asked+2, recordCount(t, walCommitQCs, stageAsked))
		require.Equal(t, persisted+2, recordCount(t, walCommitQCs, stagePersisted))

		require.NoError(t, cp.PruneAndPersist(0, []*types.CommitQC{qcs[0]}))
		require.Equal(t, asked+2, recordCount(t, walCommitQCs, stageAsked))
		require.Equal(t, persisted+2, recordCount(t, walCommitQCs, stagePersisted))
	})
}
