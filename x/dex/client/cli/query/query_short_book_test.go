package query_test

import (
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/testutil/network"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/client/cli/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	tmcli "github.com/tendermint/tendermint/libs/cli"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "usdc",
		AssetDenom: "atom",
	}
}

func networkWithShortBookObjects(t *testing.T, n int) (*network.Network, []types.ShortBook) {
	t.Helper()
	cfg := network.DefaultConfig()
	state := types.GenesisState{}
	shortBookList := []types.ShortBook{}
	require.NoError(t, cfg.Codec.UnmarshalJSON(cfg.GenesisState[types.ModuleName], &state))

	for i := 0; i < n; i++ {
		shortBook := types.ShortBook{
			Price: sdk.NewDec(int64(1 + i)),
			Entry: &types.OrderEntry{
				Price:       sdk.NewDec(int64(1 + i)),
				Quantity:    sdk.NewDec(int64(i)),
				Allocations: []*types.Allocation{{Account: "abc", Quantity: sdk.NewDec(int64(i)), OrderId: 1}},
				PriceDenom:  TEST_PAIR().PriceDenom,
				AssetDenom:  TEST_PAIR().AssetDenom,
			},
		}
		nullify.Fill(&shortBook)
		shortBookList = append(shortBookList, shortBook)
	}

	contractInfo := types.ContractInfoV2{
		CodeId:       uint64(1),
		ContractAddr: "sei1ghd753shjuwexxywmgs4xz7x2q732vcnkm6h2pyv9s6ah3hylvrqladqwc",
	}
	contractState := []types.ContractState{
		{
			ShortBookList: shortBookList,
			ContractInfo:  contractInfo,
		},
	}
	state.ContractState = contractState
	buf, err := cfg.Codec.MarshalJSON(&state)
	require.NoError(t, err)
	cfg.GenesisState[types.ModuleName] = buf
	return network.New(t, cfg), state.ContractState[0].ShortBookList
}

func TestShowShortBook(t *testing.T) {
	net, objs := networkWithShortBookObjects(t, 2)

	ctx := net.Validators[0].ClientCtx
	common := []string{
		fmt.Sprintf("--%s=json", tmcli.OutputFlag),
	}
	for _, tc := range []struct {
		desc  string
		price string
		args  []string
		err   error
		obj   types.ShortBook
	}{
		{
			desc:  "found",
			price: objs[1].Entry.Price.String(),
			args:  common,
			obj:   objs[1],
		},
		{
			desc:  "not found",
			price: "not_found",
			args:  common,
			err:   status.Error(codes.InvalidArgument, "not found"),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			args := []string{"sei1ghd753shjuwexxywmgs4xz7x2q732vcnkm6h2pyv9s6ah3hylvrqladqwc", tc.price, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom}
			args = append(args, tc.args...)
			out, err := clitestutil.ExecTestCLICmd(ctx, query.CmdShowShortBook(), args)
			if tc.err != nil {
				stat, ok := status.FromError(tc.err)
				require.True(t, ok)
				require.ErrorIs(t, stat.Err(), tc.err)
			} else {
				require.NoError(t, err)
				var resp types.QueryGetShortBookResponse
				require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
				require.NotNil(t, resp.ShortBook)
				require.Equal(t,
					nullify.Fill(&tc.obj),
					nullify.Fill(&resp.ShortBook),
				)
			}
		})
	}
}

func TestListShortBook(t *testing.T) {
	net, objs := networkWithShortBookObjects(t, 5)

	ctx := net.Validators[0].ClientCtx
	request := func(next []byte, offset, limit uint64, total bool) []string {
		args := []string{
			"sei1ghd753shjuwexxywmgs4xz7x2q732vcnkm6h2pyv9s6ah3hylvrqladqwc", TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom,
			fmt.Sprintf("--%s=json", tmcli.OutputFlag),
		}
		if next == nil {
			args = append(args, fmt.Sprintf("--%s=%d", flags.FlagOffset, offset))
		} else {
			args = append(args, fmt.Sprintf("--%s=%s", flags.FlagPageKey, next))
		}
		args = append(args, fmt.Sprintf("--%s=%d", flags.FlagLimit, limit))
		if total {
			args = append(args, fmt.Sprintf("--%s", flags.FlagCountTotal))
		}
		return args
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(objs); i += step {
			args := request(nil, uint64(i), uint64(step), false)
			out, err := clitestutil.ExecTestCLICmd(ctx, query.CmdListShortBook(), args)
			require.NoError(t, err)
			var resp types.QueryAllShortBookResponse
			require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
			require.LessOrEqual(t, len(resp.ShortBook), step)
			require.Subset(t,
				nullify.Fill(objs),
				nullify.Fill(resp.ShortBook),
			)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(objs); i += step {
			args := request(next, 0, uint64(step), false)
			out, err := clitestutil.ExecTestCLICmd(ctx, query.CmdListShortBook(), args)
			require.NoError(t, err)
			var resp types.QueryAllShortBookResponse
			require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
			require.LessOrEqual(t, len(resp.ShortBook), step)
			require.Subset(t,
				nullify.Fill(objs),
				nullify.Fill(resp.ShortBook),
			)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		args := request(nil, 0, uint64(len(objs)), true)
		out, err := clitestutil.ExecTestCLICmd(ctx, query.CmdListShortBook(), args)
		require.NoError(t, err)
		var resp types.QueryAllShortBookResponse
		require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
		require.NoError(t, err)
		require.Equal(t, len(objs), int(resp.Pagination.Total))
		require.ElementsMatch(t,
			nullify.Fill(objs),
			nullify.Fill(resp.ShortBook),
		)
	})
}
