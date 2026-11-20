package epoch

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ClosingEpoch returns the epoch that road ends, for epochs of epochLength
// roads counted from firstHeight, if road is that epoch's last road. Otherwise
// None.
func ClosingEpoch(firstHeight, epochLength, road types.RoadHeight) utils.Option[types.EpochIndex] {
	if road < firstHeight || (road-firstHeight+1)%epochLength != 0 {
		return utils.None[types.EpochIndex]()
	}
	return utils.Some(types.EpochIndex((road - firstHeight) / epochLength))
}
