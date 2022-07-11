package cli_test

import (
	"testing"

	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/client/cli"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func TestCmdGetOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddNewOrder(ctx, types.Order{
		Id:           1,
		Account:      "accnt",
		ContractAddr: "test",
		PriceDenom:   "USDC",
		AssetDenom:   "ATOM",
		Status:       types.OrderStatus_PLACED,
		Quantity:     sdk.MustNewDecFromStr("2"),
	})
	args := []string{"genesis", tc.price, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom}
	args = append(args, tc.args...)
	out, err := clitestutil.ExecTestCLICmd(ctx, cli.CmdShowLongBook(), args)
}
