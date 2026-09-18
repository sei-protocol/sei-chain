package core

import (
	"errors"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrNewHeadsUnavailable is returned by ExecutedHeights on nodes that do not
// execute blocks through Autobahn.
var ErrNewHeadsUnavailable = errors.New("newHeads subscriptions require Autobahn execution")

// ExecutedHeights returns the Giga router's executed-block watch.
func (env *Environment) ExecutedHeights() (utils.AtomicRecv[atypes.GlobalBlockNumber], error) {
	giga, ok := env.gigaRouter().Get()
	if !ok {
		return utils.AtomicRecv[atypes.GlobalBlockNumber]{}, ErrNewHeadsUnavailable
	}
	return giga.ExecutedHeights(), nil
}
