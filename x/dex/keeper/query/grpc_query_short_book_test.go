package query_test

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
	keeperquery "github.com/sei-protocol/sei-chain/x/dex/keeper/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func TestShortBookQuerySingle(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wrapper := keeperquery.KeeperWrapper{Keeper: keeper}
	wctx := sdk.WrapSDKContext(ctx)
	msgs := keepertest.CreateNShortBook(keeper, ctx, 2)
	for _, tc := range []struct {
		desc     string
		request  *types.QueryGetShortBookRequest
		response *types.QueryGetShortBookResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetShortBookRequest{Price: msgs[0].Price.String(), ContractAddr: keepertest.TestContract, PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom},
			response: &types.QueryGetShortBookResponse{ShortBook: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetShortBookRequest{Price: msgs[1].Price.String(), ContractAddr: keepertest.TestContract, PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom},
			response: &types.QueryGetShortBookResponse{ShortBook: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetShortBookRequest{Price: "10000", ContractAddr: keepertest.TestContract, PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := wrapper.ShortBook(wctx, tc.request)
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

func TestShortBookQueryPaginated(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wrapper := keeperquery.KeeperWrapper{Keeper: keeper}
	wctx := sdk.WrapSDKContext(ctx)
	msgs := keepertest.CreateNShortBook(keeper, ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllShortBookRequest {
		return &types.QueryAllShortBookRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
			ContractAddr: keepertest.TestContract,
			PriceDenom:   keepertest.TestPriceDenom,
			AssetDenom:   keepertest.TestAssetDenom,
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := wrapper.ShortBookAll(wctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ShortBook), step)
			require.Subset(t,
				nullify.Fill(msgs),
				nullify.Fill(resp.ShortBook),
			)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := wrapper.ShortBookAll(wctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ShortBook), step)
			require.Subset(t,
				nullify.Fill(msgs),
				nullify.Fill(resp.ShortBook),
			)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := wrapper.ShortBookAll(wctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.ElementsMatch(t,
			nullify.Fill(msgs),
			nullify.Fill(resp.ShortBook),
		)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := wrapper.ShortBookAll(wctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}
