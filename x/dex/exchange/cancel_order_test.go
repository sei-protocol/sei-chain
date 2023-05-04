package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TestCancelOrder(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(98),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(98),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(101),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(101),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  7,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}

	// 2 Dirty long entries before cancellation
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 0)
	// Allocations before cancellation contains id 5
	assert.Equal(t, (*longBook[0].GetEntry()).Allocations, []*types.Allocation{{
		OrderId:  5,
		Account:  "abc",
		Quantity: sdk.NewDec(5),
	}})

	// Cancel Long Id 5
	cancellation := types.Cancellation{
		Id:                5,
		Creator:           "abc",
		ContractAddr:      "test",
		PriceDenom:        "USDC",
		AssetDenom:        "ATOM",
		PositionDirection: types.PositionDirection_LONG,
		Price:             sdk.NewDec(98),
	}
	cancels := []*types.Cancellation{&cancellation}
	exchange.CancelOrders(
		cancels, &orderbook,
	)

	// 3 Dirty long entries after cancellation
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 1)
	// No allocations after cancellation
	assert.Equal(t, (*longBook[0].GetEntry()).Allocations, []*types.Allocation{})

}
