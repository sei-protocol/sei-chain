package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func MigrateRemoveCurrBlockBaseFee(ctx sdk.Context, k *keeper.Keeper) error {
	currBlockBaseFee := k.GetCurrBaseFeePerGas(ctx)
	k.SetNextBaseFeePerGas(ctx, currBlockBaseFee)
	// just store min base fee in curr block base fee
	k.SetCurrBaseFeePerGas(ctx, types.DefaultMinFeePerGas)
	return nil
}
