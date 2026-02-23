package testutil

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/cli"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	clitestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil/cli"
	bankcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/client/cli"
)

func MsgSendExec(clientCtx client.Context, from, to, amount fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := make([]string, 0, 3+len(extraArgs))
	args = append(args, from.String(), to.String(), amount.String())
	args = append(args, extraArgs...)

	return clitestutil.ExecTestCLICmd(clientCtx, bankcli.NewSendTxCmd(), args)
}

func QueryBalancesExec(clientCtx client.Context, address fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {
	args := make([]string, 0, 2+len(extraArgs))
	args = append(args, address.String(), fmt.Sprintf("--%s=json", cli.OutputFlag))
	args = append(args, extraArgs...)

	return clitestutil.ExecTestCLICmd(clientCtx, bankcli.GetBalancesCmd(), args)
}
