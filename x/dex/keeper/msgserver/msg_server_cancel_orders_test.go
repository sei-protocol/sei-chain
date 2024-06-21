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

func TestCancelOrder(t *testing.T) {
	// store a long limit order to the orderbook
	keeper, ctx := keepertest.DexKeeper(t)

	// cancel order
	msg := &types.MsgCancelOrders{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		Cancellations: []*types.Cancellation{
			{
				Price:             sdk.OneDec(),
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
				Id:                1,
			},
		},
	}
	wctx := sdk.WrapSDKContext(ctx)
	server := msgserver.NewMsgServerImpl(*keeper)
	_, err := server.CancelOrders(wctx, msg)
	require.EqualError(t, err, sdkerrors.Wrapf(sdkerrors.ErrNotSupported, "deprecated").Error())
}
