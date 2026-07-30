package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// appProgress owns the in-memory AppQC tip and AppVote accumulators.
type appProgress struct {
	latestAppQC utils.Option[*types.AppQC]
	votes       *queue[types.GlobalBlockNumber, appVotes]
}
