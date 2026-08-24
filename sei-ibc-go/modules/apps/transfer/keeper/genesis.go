package keeper

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

// InitGenesis initializes the ibc-transfer state.
func (k Keeper) InitGenesis(ctx sdk.Context, state types.GenesisState) {
	k.SetPort(ctx, state.PortId)

	for _, trace := range state.DenomTraces {
		k.SetDenomTrace(ctx, trace)
	}

	k.SetParams(ctx, state.Params)

	// check if the module account exists
	moduleAcc := k.GetTransferAccount(ctx)
	if moduleAcc == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}
}

// ExportGenesis exports ibc-transfer module's portID and denom trace info into its genesis state.
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	return &types.GenesisState{
		PortId:      k.GetPort(ctx),
		DenomTraces: k.GetAllDenomTraces(ctx),
		Params:      k.GetParams(ctx),
	}
}
