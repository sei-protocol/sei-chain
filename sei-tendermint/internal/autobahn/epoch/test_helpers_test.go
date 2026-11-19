package epoch

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func testEpoch(idx types.EpochIndex, firstRoad types.RoadIndex, committee *types.Committee) *types.Epoch {
	first := firstRoad + types.RoadIndex(idx)*epochLength
	return types.NewEpoch(idx, types.RoadRange{First: first, Next: first + epochLength}, committee)
}

func testPersistedRegistry(live []*types.Epoch) *persistedRegistry {
	epochs := make(map[types.EpochIndex]*types.Epoch, len(live))
	for _, epoch := range live {
		epochs[epoch.EpochIndex()] = epoch
	}
	return &persistedRegistry{live: epochs, liveRange: types.EpochRange{First: live[0].EpochIndex(), Next: live[len(live)-1].EpochIndex() + 1}}
}

func testNewRegistry(t *testing.T, committee *types.Committee, firstRoad types.RoadIndex, snapshot *pb.PersistedEpochRegistry) (*Registry, error) {
	t.Helper()
	row := utils.None[[]byte]()
	if snapshot != nil {
		row = utils.Some(protoutils.Marshal(snapshot))
	}
	return NewRegistry(committee, firstRoad, time.Time{}, row)
}
