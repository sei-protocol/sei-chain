package simulation

import (
	"math/rand"

	simtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/simulation"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
)

// GenConnectionGenesis returns the default connection genesis state.
func GenConnectionGenesis(_ *rand.Rand, _ []simtypes.Account) types.GenesisState {
	return types.DefaultGenesisState()
}
