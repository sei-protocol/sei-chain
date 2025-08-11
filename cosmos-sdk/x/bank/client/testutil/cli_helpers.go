package testutil

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/tendermint/libs/cli"

	"github.com/sei-protocol/sei-chain/cosmos-sdk/client"
	"github.com/sei-protocol/sei-chain/cosmos-sdk/testutil"
	clitestutil "github.com/sei-protocol/sei-chain/cosmos-sdk/testutil/cli"
	bankcli "github.com/sei-protocol/sei-chain/cosmos-sdk/x/bank/client/cli"
)

func MsgSendExec(clientCtx client.Context, from, to, amount fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := []string{from.String(), to.String(), amount.String()}
	args = append(args, extraArgs...)

	return clitestutil.ExecTestCLICmd(clientCtx, bankcli.NewSendTxCmd(), args)
}

func QueryBalancesExec(clientCtx client.Context, address fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := []string{address.String(), fmt.Sprintf("--%s=json", cli.OutputFlag)}
	args = append(args, extraArgs...)

	return clitestutil.ExecTestCLICmd(clientCtx, bankcli.GetBalancesCmd(), args)
}
