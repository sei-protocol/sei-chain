package utils

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func SetPriceStateFromExecutionOutcome(
	ctx sdk.Context,
	keeper *keeper.Keeper,
	contractAddr types.ContractAddress,
	pair types.Pair,
	outcome exchange.ExecutionOutcome,
) {
	if outcome.TotalQuantity.IsZero() {
		return
	}

	avgPrice := outcome.TotalNotional.Quo(outcome.TotalQuantity)
	priceState := types.Price{
		Pair:                       &pair,
		Price:                      avgPrice,
		SnapshotTimestampInSeconds: uint64(ctx.BlockTime().Unix()),
	}
	keeper.SetPriceState(ctx, priceState, string(contractAddr))
}
