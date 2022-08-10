package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

func (k Keeper) BeforeEpochStart(ctx sdk.Context, epoch types.Epoch) {
}

func (k Keeper) AfterEpochEnd(ctx sdk.Context, epoch types.Epoch) {
	// minter := k.GetMinter(ctx)
	// params := k.GetParams(ctx)
	// if epoch.CurrentEpochStartTime >= params.Red
}
