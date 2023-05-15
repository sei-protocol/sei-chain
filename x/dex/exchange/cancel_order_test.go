package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TestCancelOrder(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
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
	for _, s := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", s)
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(101),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(101),
				Quantity: sdk.NewDec(12),
				Allocations: []*types.Allocation{{
					OrderId:  7,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}, {
					OrderId:  8,
					Account:  "def",
					Quantity: sdk.NewDec(7),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	for _, s := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", s)
	}

	// Allocations before cancellation contains id 5
	assert.Equal(t, (*longBook[0].GetOrderEntry()).Allocations, []*types.Allocation{{
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
	// Cancel short Id 7
	cancellationShort := types.Cancellation{
		Id:                7,
		Creator:           "abc",
		ContractAddr:      "test",
		PriceDenom:        "USDC",
		AssetDenom:        "ATOM",
		PositionDirection: types.PositionDirection_SHORT,
		Price:             sdk.NewDec(101),
	}
	cancels := []*types.Cancellation{&cancellation, &cancellationShort}
	exchange.CancelOrders(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"},
		cancels,
	)

	// No allocations after cancellation
	_, found := dexkeeper.GetLongBookByPrice(ctx, "test", sdk.NewDec(98), "USDC", "ATOM")
	require.False(t, found)
	entry, found := dexkeeper.GetShortBookByPrice(ctx, "test", sdk.NewDec(101), "USDC", "ATOM")
	require.True(t, found)
	require.Equal(t, sdk.NewDec(7), entry.GetOrderEntry().Quantity)
	require.Equal(t, []*types.Allocation{{
		OrderId:  8,
		Account:  "def",
		Quantity: sdk.NewDec(7),
	}}, entry.GetOrderEntry().Allocations)
}
