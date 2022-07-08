package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
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
			request:  &types.QueryGetSettlementsRequest{ContractAddr: TEST_CONTRACT, PriceDenom: "usdc0", AssetDenom: "sei0", BlockHeight: uint64(ctx.BlockHeight())},
			response: &types.QueryGetSettlementsResponse{Settlements: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetSettlementsRequest{ContractAddr: TEST_CONTRACT, PriceDenom: "usdc1", AssetDenom: "sei1", BlockHeight: uint64(ctx.BlockHeight())},
			response: &types.QueryGetSettlementsResponse{Settlements: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetSettlementsRequest{ContractAddr: TEST_CONTRACT, PriceDenom: "btc", AssetDenom: "sei", BlockHeight: uint64(ctx.BlockHeight())},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := keeper.Settlements(wctx, tc.request)
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

func TestSettlementsQueryPaginated(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	msgs := createNSettlements(keeper, ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllSettlementsRequest {
		return &types.QueryAllSettlementsRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := keeper.SettlementsAll(wctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Settlements), step)
			require.Subset(t,
				nullify.Fill(msgs),
				nullify.Fill(resp.Settlements),
			)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := keeper.SettlementsAll(wctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Settlements), step)
			require.Subset(t,
				nullify.Fill(msgs),
				nullify.Fill(resp.Settlements),
			)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, _ := keeper.SettlementsAll(wctx, request(nil, 0, 0, true))
		// require.NoError(t, err)
		// require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.ElementsMatch(t,
			nullify.Fill(msgs),
			nullify.Fill(resp.Settlements),
		)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := keeper.SettlementsAll(wctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
