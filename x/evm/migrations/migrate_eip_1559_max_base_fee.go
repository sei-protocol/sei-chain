package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MigrateEip1559MaxFeePerGas(ctx sdk.Context, k *keeper.Keeper) error {
	keeperParams := k.GetParamsIfExists(ctx)
	keeperParams.MaximumFeePerGas = types.DefaultParams().MaximumFeePerGas
	k.SetParams(ctx, keeperParams)
	return nil
}
