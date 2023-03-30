package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k KeeperWrapper) GetTwaps(goCtx context.Context, req *types.QueryGetTwapsRequest) (*types.QueryGetTwapsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	allRegisteredPairs := k.GetAllRegisteredPairs(ctx, req.ContractAddr)
	twaps := []*types.Twap{}
	for _, pair := range allRegisteredPairs {
		twapPairStartTime := time.Now().UnixMicro()
		prices := k.GetAllPrices(ctx, req.ContractAddr, pair)
		twaps = append(twaps, &types.Twap{
			Pair:            &pair, //nolint:gosec,exportloopref // USING THE POINTER HERE COULD BE BAD, LET'S CHECK IT.
			Twap:            calculateTwap(ctx, prices, req.LookbackSeconds),
			LookbackSeconds: req.LookbackSeconds,
		})
		twapPairEndTime := time.Now().UnixMicro()
		ctx.Logger().Info(fmt.Sprintf("[Seichain-Debug] GetTwap pair priceDenom %s, assetDenom: %s, num prices %d, lookBack Seconds %d,  latency is %d", pair.PriceDenom, pair.AssetDenom, len(prices), req.LookbackSeconds, twapPairEndTime-twapPairStartTime))
	}

	return &types.QueryGetTwapsResponse{
		Twaps: twaps,
	}, nil
}

func calculateTwap(ctx sdk.Context, prices []*types.Price, lookback uint64) sdk.Dec {
	// sort prices in descending order to start iteration from the latest
	sort.Slice(prices, func(p1, p2 int) bool {
		return prices[p1].SnapshotTimestampInSeconds > prices[p2].SnapshotTimestampInSeconds
	})
	var timeTraversed uint64
	weightedPriceSum := sdk.ZeroDec()
	for _, price := range prices {
		newTimeTraversed := uint64(ctx.BlockTime().Unix()) - price.SnapshotTimestampInSeconds
		if newTimeTraversed > lookback {
			weightedPriceSum = weightedPriceSum.Add(
				price.Price.MulInt64(int64(lookback - timeTraversed)),
			)
			timeTraversed = lookback
			break
		}
		weightedPriceSum = weightedPriceSum.Add(
			price.Price.MulInt64(int64(newTimeTraversed - timeTraversed)),
		)
		timeTraversed = newTimeTraversed
	}
	if timeTraversed == 0 {
		return sdk.ZeroDec()
	}
	return weightedPriceSum.QuoInt64(int64(timeTraversed))
}
