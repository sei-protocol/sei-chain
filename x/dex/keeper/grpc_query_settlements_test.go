package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func TestSettlementsQuerySingle(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	msgs := createNSettlements(keeper, ctx, 2)
	for _, tc := range []struct {
		desc     string
		request  *types.QueryGetSettlementsRequest
		response *types.QueryGetSettlementsResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetSettlementsRequest{ContractAddr: TEST_CONTRACT, PriceDenom: "usdc0", AssetDenom: "sei0", OrderId: 0},
			response: &types.QueryGetSettlementsResponse{Settlements: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetSettlementsRequest{ContractAddr: TEST_CONTRACT, PriceDenom: "usdc1", AssetDenom: "sei1", OrderId: 1},
			response: &types.QueryGetSettlementsResponse{Settlements: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetSettlementsRequest{ContractAddr: TEST_CONTRACT, PriceDenom: "btc", AssetDenom: "sei", OrderId: 2},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := keeper.GetSettlements(wctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.Equal(t,
					nullify.Fill(tc.response),
					nullify.Fill(response),
				)
			}
		})
	}
}
