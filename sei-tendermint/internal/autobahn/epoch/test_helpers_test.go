package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type errPersister struct{ err error }

func (e errPersister) Persist(*pb.PersistedEpochRegistry) error { return e.err }

func testEpoch(idx types.EpochIndex, firstRoad types.RoadIndex, committee *types.Committee) *types.Epoch {
	first := firstRoad + types.RoadIndex(idx)*epochLength
	return types.NewEpoch(idx, types.RoadRange{First: first, Next: first + epochLength}, committee)
}

func testPersistedRegistry(live []*types.Epoch, pending utils.Option[*types.Epoch]) *persistedRegistry {
	epochs := make(map[types.EpochIndex]*types.Epoch, len(live))
	for _, epoch := range live {
		epochs[epoch.EpochIndex()] = epoch
	}
	return &persistedRegistry{live: epochs, liveRange: types.EpochRange{First: live[0].EpochIndex(), Next: live[len(live)-1].EpochIndex() + 1}, pending: pending}
}

func testNewRegistry(t *testing.T, committee *types.Committee, firstRoad types.RoadIndex, snapshot *pb.PersistedEpochRegistry) (*Registry, error) {
	t.Helper()
	dir := utils.Some(t.TempDir())
	persister, _, err := openEpochSnapshot(dir)
	require.NoError(t, err)
	require.NoError(t, persister.Persist(snapshot))
	return NewRegistry(committee, firstRoad, time.Time{}, dir)
}
