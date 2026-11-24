package core

import (
	"errors"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ErrNewHeadsUnavailable is returned by ExecutedBlocks on nodes that do not
// execute blocks through Autobahn.
var ErrNewHeadsUnavailable = errors.New("newHeads subscriptions require Autobahn execution")

// ExecutedBlocks returns the Autobahn executor's executed-block watch.
func (env *GigaEnvironment) ExecutedBlocks() (utils.AtomicRecv[atypes.ExecutedBlocks], error) {
	executor, ok := env.Executor.Get()
	if !ok {
		return utils.AtomicRecv[atypes.ExecutedBlocks]{}, ErrNewHeadsUnavailable
	}
	return executor.ExecutedBlocks(), nil
}
