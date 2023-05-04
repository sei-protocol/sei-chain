package query

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (k KeeperWrapper) GetMarketSummary(goCtx context.Context, req *types.QueryGetMarketSummaryRequest) (*types.QueryGetMarketSummaryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	prices := k.GetAllPrices(ctx, req.ContractAddr, types.Pair{PriceDenom: req.PriceDenom, AssetDenom: req.AssetDenom})
	cutoff := ctx.BlockTime().Unix() - int64(req.LookbackInSeconds)
	maxPrice := sdk.ZeroDec()
	minPrice := sdk.ZeroDec()
	latestTimestamp := 0
	lastPrice := sdk.ZeroDec()
	for _, price := range prices {
		if price.SnapshotTimestampInSeconds < uint64(cutoff) {
			continue
		}
		if maxPrice.IsZero() || price.Price.GT(maxPrice) {
			maxPrice = price.Price
		}
		if minPrice.IsZero() || price.Price.LT(minPrice) {
			minPrice = price.Price
		}
		if price.SnapshotTimestampInSeconds > uint64(latestTimestamp) {
			latestTimestamp = int(price.SnapshotTimestampInSeconds)
			lastPrice = price.Price
		}
	}

	zero := sdk.ZeroDec()
	return &types.QueryGetMarketSummaryResponse{
		TotalVolume:         &zero, // TODO: replace once we start tracking volume
		TotalVolumeNotional: &zero, // TODO: replace once we start tracking volume
		HighPrice:           &maxPrice,
		LowPrice:            &minPrice,
		LastPrice:           &lastPrice,
	}, nil
}
