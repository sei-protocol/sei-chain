package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k KeeperWrapper) GetTwaps(goCtx context.Context, req *types.QueryGetTwapsRequest) (*types.QueryGetTwapsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	allRegisteredPairs := k.GetAllRegisteredPairs(ctx, req.ContractAddr)
	twaps := []*types.Twap{}
	for _, pair := range allRegisteredPairs {
		prices := k.GetPricesForTwap(ctx, req.ContractAddr, pair, req.LookbackSeconds)
		twaps = append(twaps, &types.Twap{
			Pair:            &pair, //nolint:gosec,exportloopref // USING THE POINTER HERE COULD BE BAD, LET'S CHECK IT.
			Twap:            calculateTwap(ctx, prices, req.LookbackSeconds),
			LookbackSeconds: req.LookbackSeconds,
		})
	}

	return &types.QueryGetTwapsResponse{
		Twaps: twaps,
	}, nil
}

func calculateTwap(ctx sdk.Context, prices []*types.Price, lookback uint64) sdk.Dec {
	if len(prices) == 0 {
		return sdk.ZeroDec()
	}
	if len(prices) == 1 {
		return prices[0].Price
	}
	weightedPriceSum := sdk.ZeroDec()
	lastTimestamp := ctx.BlockTime().Unix()
	for _, price := range prices {
		weightedPriceSum = weightedPriceSum.Add(
			price.Price.MulInt64(lastTimestamp - int64(price.SnapshotTimestampInSeconds)),
		)
		lastTimestamp = int64(price.SnapshotTimestampInSeconds)
	}
	// not possible for division by 0 here since prices have unique timestamps
	totalTimeSpan := ctx.BlockTime().Unix() - int64(prices[len(prices)-1].SnapshotTimestampInSeconds)
	return weightedPriceSum.QuoInt64(totalTimeSpan)
}
