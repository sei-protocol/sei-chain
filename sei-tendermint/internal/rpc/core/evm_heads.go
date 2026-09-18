package core

import (
	"errors"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrNewHeadsUnavailable is returned by ExecutedBlocks on nodes that do not
// execute blocks through Autobahn.
var ErrNewHeadsUnavailable = errors.New("newHeads subscriptions require Autobahn execution")

// ExecutedBlocks returns the Giga router's executed-block watch.
func (env *Environment) ExecutedBlocks() (utils.AtomicRecv[atypes.ExecutedBlock], error) {
	giga, ok := env.gigaRouter().Get()
	if !ok {
		return utils.AtomicRecv[atypes.ExecutedBlock]{}, ErrNewHeadsUnavailable
	}
	return giga.ExecutedBlocks(), nil
}
