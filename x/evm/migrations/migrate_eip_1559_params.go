package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MigrateEip1559Params(ctx sdk.Context, k *keeper.Keeper) error {
	keeperParams := k.GetParamsIfExists(ctx)
	keeperParams.MaxDynamicBaseFeeUpwardAdjustment = types.DefaultParams().MaxDynamicBaseFeeUpwardAdjustment
	keeperParams.MaxDynamicBaseFeeDownwardAdjustment = types.DefaultParams().MaxDynamicBaseFeeDownwardAdjustment
	k.SetParams(ctx, keeperParams)
	return nil
}
