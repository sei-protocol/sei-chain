package migrations

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func V12ToV13(ctx sdk.Context, dexkeeper keeper.Keeper) error {
	oldParams := dexkeeper.GetParams(ctx)

	newParams := types.DefaultParams()

	newParams.BeginBlockGasLimit = oldParams.BeginBlockGasLimit
	newParams.EndBlockGasLimit = oldParams.EndBlockGasLimit
	newParams.DefaultGasPerOrder = oldParams.DefaultGasPerOrder
	newParams.DefaultGasPerCancel = oldParams.DefaultGasPerCancel
	newParams.MinRentDeposit = oldParams.MinRentDeposit
	newParams.PriceSnapshotRetention = oldParams.PriceSnapshotRetention
	newParams.SudoCallGasPrice = oldParams.SudoCallGasPrice

	dexkeeper.SetParams(ctx, newParams)
	return nil
}
