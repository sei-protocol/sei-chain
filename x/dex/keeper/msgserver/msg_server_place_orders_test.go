package msgserver_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const (
	TestCreator  = "sei1ewxvf5a9wq9zk5nurtl6m9yfxpnhyp7s7uk5sl"
	TestContract = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
)

func TestPlaceOrder(t *testing.T) {
	msg := &types.MsgPlaceOrders{
		Creator:      TestCreator,
		ContractAddr: TestContract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("10"),
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
			},
			{
				Price:             sdk.MustNewDecFromStr("20"),
				Quantity:          sdk.MustNewDecFromStr("5"),
				Data:              "",
				PositionDirection: types.PositionDirection_SHORT,
				OrderType:         types.OrderType_MARKET,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
			},
		},
	}
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	_, err := server.PlaceOrders(wctx, msg)
	require.EqualError(t, err, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "deprecated").Error())
}
