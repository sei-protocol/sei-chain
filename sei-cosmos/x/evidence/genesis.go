package evidence

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/exported"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/evidence/types"
)

// InitGenesis initializes the evidence module's state from a provided genesis
// state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, gs *types.GenesisState) {
	if err := gs.Validate(); err != nil {
		panic(fmt.Sprintf("failed to validate %s genesis state: %s", types.ModuleName, err))
	}

	for _, e := range gs.Evidence {
		evi, ok := e.GetCachedValue().(exported.Evidence)
		if !ok {
			panic("expected evidence")
		}
		if _, ok := k.GetEvidence(ctx, evi.Hash()); ok {
			panic(fmt.Sprintf("evidence with hash %s already exists", evi.Hash()))
		}

		k.SetEvidence(ctx, evi)
	}
}
