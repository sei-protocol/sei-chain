package avail

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// epochProgress owns the active Prev|Current admission window.
type epochProgress struct {
	duo utils.AtomicSend[types.EpochDuo] // Store under Lock; State holds Recv
}
