package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

func (k *Keeper) InitGenesis(ctx sdk.Context, genState types.GenesisState) {
	moduleAcc := authtypes.NewEmptyModuleAccount(types.ModuleName)
	k.accountKeeper.SetModuleAccount(ctx, moduleAcc)

	// TODO: Fill in more genesis stuff here
}
