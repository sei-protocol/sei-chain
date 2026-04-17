package simulation

import (
	"math/rand"

	simtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/simulation"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
)

// GenClientGenesis returns the default client genesis state.
func GenClientGenesis(_ *rand.Rand, _ []simtypes.Account) types.GenesisState {
	return types.DefaultGenesisState()
}
