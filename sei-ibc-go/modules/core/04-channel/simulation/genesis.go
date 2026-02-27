package simulation

import (
	"math/rand"

	simtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/simulation"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
)

// GenChannelGenesis returns the default channel genesis state.
func GenChannelGenesis(_ *rand.Rand, _ []simtypes.Account) types.GenesisState {
	return types.DefaultGenesisState()
}
