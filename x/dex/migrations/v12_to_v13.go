package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V12ToV13(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	//prevParams := dexkeeper.GetParams(ctx)
	//prevParams.GasAllowancePerSettlement = types.DefaultGasAllowancePerSettlement
	//prevParams.MinProcessableRent = types.DefaultMinProcessableRent

	//dexkeeper.SetParams(ctx, prevParams)
	defaultParams := types.DefaultParams()
	dexkeeper.SetParams(ctx, defaultParams)
	return nil
}
