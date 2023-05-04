package query

import (
	"context"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ZeroPrice = sdk.ZeroDec()

func (k KeeperWrapper) GetHistoricalPrices(goCtx context.Context, req *types.QueryGetHistoricalPricesRequest) (*types.QueryGetHistoricalPricesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	prices := k.GetAllPrices(ctx, req.ContractAddr, types.Pair{PriceDenom: req.PriceDenom, AssetDenom: req.AssetDenom})
	currentTimeStamp := uint64(ctx.BlockTime().Unix())
	beginTimestamp := currentTimeStamp - req.NumOfPeriods*req.PeriodLengthInSeconds
	// sort descending
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].SnapshotTimestampInSeconds > prices[j].SnapshotTimestampInSeconds
	})
	validPrices := []*types.Price{}
	for _, price := range prices {
		// append early so that we include the latest price before beginTimestamp, since
		// we need it to set the open price of the first period.
		if price.SnapshotTimestampInSeconds < currentTimeStamp {
			validPrices = append(validPrices, price)
		}
		if price.SnapshotTimestampInSeconds < beginTimestamp {
			break
		}
	}

	candlesticks := make([]*types.PriceCandlestick, req.NumOfPeriods)

	// set timestamp
	for i := range candlesticks {
		candlesticks[i] = &types.PriceCandlestick{}
		candlesticks[i].EndTimestamp = currentTimeStamp - uint64(i)*req.PeriodLengthInSeconds
		candlesticks[i].BeginTimestamp = candlesticks[i].EndTimestamp - req.PeriodLengthInSeconds
	}

	// set open
	pricePtr := 0
	for i := range candlesticks {
		for pricePtr < len(validPrices) && validPrices[pricePtr].SnapshotTimestampInSeconds > candlesticks[i].BeginTimestamp {
			pricePtr++
		}
		if pricePtr < len(validPrices) {
			candlesticks[i].Open = &validPrices[pricePtr].Price
		} else {
			// this would happen if the earliest price point available is after the begin timestamp
			candlesticks[i].Open = &ZeroPrice
		}
	}

	// set close
	pricePtr = 0
	for i := range candlesticks {
		for pricePtr < len(validPrices) && validPrices[pricePtr].SnapshotTimestampInSeconds >= candlesticks[i].EndTimestamp {
			pricePtr++
		}
		if pricePtr < len(validPrices) {
			candlesticks[i].Close = &validPrices[pricePtr].Price
		} else {
			// this would happen if the earliest price point available is after the first end timestamp
			candlesticks[i].Close = &ZeroPrice
		}
	}

	// set high
	pricePtr = 0
	for i := range candlesticks {
		// initialize to the open price
		candlesticks[i].High = candlesticks[i].Open
		set := false
		for pricePtr < len(validPrices) {
			price := validPrices[pricePtr]
			if price.SnapshotTimestampInSeconds < candlesticks[i].BeginTimestamp || price.SnapshotTimestampInSeconds >= candlesticks[i].EndTimestamp {
				break
			}
			if !set || price.Price.GT(*candlesticks[i].High) {
				set = true
				candlesticks[i].High = &price.Price
			}
			pricePtr++
		}
	}

	// set low
	pricePtr = 0
	for i := range candlesticks {
		// initialize to the open price
		candlesticks[i].Low = candlesticks[i].Open
		set := false
		for pricePtr < len(validPrices) {
			price := validPrices[pricePtr]
			if price.SnapshotTimestampInSeconds < candlesticks[i].BeginTimestamp || price.SnapshotTimestampInSeconds >= candlesticks[i].EndTimestamp {
				break
			}
			if !set || price.Price.LT(*candlesticks[i].Low) {
				set = true
				candlesticks[i].Low = &price.Price
			}
			pricePtr++
		}
	}

	return &types.QueryGetHistoricalPricesResponse{
		Prices: candlesticks,
	}, nil
}
