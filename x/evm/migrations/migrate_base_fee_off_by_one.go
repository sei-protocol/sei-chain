package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

func MigrateBaseFeeOffByOne(ctx sdk.Context, k *keeper.Keeper) error {
	baseFee := k.GetDynamicBaseFeePerGas(ctx)
	k.SetPrevBlockBaseFeePerGas(ctx, baseFee)
	return nil
}
