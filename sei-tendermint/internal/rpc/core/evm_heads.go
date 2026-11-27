package core

import (
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// ExecutedBlocks returns the Autobahn executor's executed-block watch.
func (env *GigaEnvironment) ExecutedBlocks() utils.AtomicRecv[atypes.ExecutedBlocks] {
	return env.Executor.ExecutedBlocks()
}
