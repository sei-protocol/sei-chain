package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	fuzzutils "github.com/sei-protocol/sei-chain/testutil/fuzzing"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func FuzzPlaceOrders(f *testing.F) {
	f.Add(uint64(0), int32(0), 2, 2, int64(10), false, int64(2), false, keepertest.TestPriceDenom, keepertest.TestAssetDenom, int32(0), int32(0), "", "", false, int64(20))
	f.Fuzz(fuzzTargetPlaceOrders)
}

func fuzzTargetPlaceOrders(
	t *testing.T,
	id uint64,
	status int32,
	accountIdx int,
	contractIdx int,
	priceI int64,
	priceIsNil bool,
	quantityI int64,
	quantityIsNil bool,
	priceDenom string,
	assetDenom string,
	orderType int32,
	positionDirection int32,
	data string,
	statusDescription string,
	fundIsNil bool,
	fundAmount int64,
) {
	keeper, ctx := keepertest.DexKeeper(t)
	contract := fuzzutils.GetContract(contractIdx)
	keeper.AddRegisteredPair(ctx, contract, keepertest.TestPair)
	keeper.SetPriceTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.PriceTicksize)
	keeper.SetQuantityTickSizeForPair(ctx, TestContract, keepertest.TestPair, *keepertest.TestPair.QuantityTicksize)
	wctx := sdk.WrapSDKContext(ctx)
	msg := &types.MsgPlaceOrders{
		Creator:      fuzzutils.GetAccount(accountIdx),
		ContractAddr: contract,
		Orders: []*types.Order{
			{
				Id:                id,
				Status:            types.OrderStatus(status),
				Price:             fuzzutils.FuzzDec(priceI, priceIsNil),
				Quantity:          fuzzutils.FuzzDec(quantityI, quantityIsNil),
				Data:              data,
				StatusDescription: statusDescription,
				PositionDirection: types.PositionDirection(positionDirection),
				OrderType:         types.OrderType(orderType),
				PriceDenom:        priceDenom,
				AssetDenom:        assetDenom,
			},
		},
		Funds: []sdk.Coin{fuzzutils.FuzzCoin(priceDenom, fundIsNil, fundAmount)},
	}
	server := msgserver.NewMsgServerImpl(*keeper)
	require.NotPanics(t, func() { server.PlaceOrders(wctx, msg) })
}
