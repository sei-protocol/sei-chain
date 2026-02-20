package testutil

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	clitestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil/cli"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/client/cli"
)

var commonArgs = []string{
	fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
	fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
	fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.NewInt(10))).String()),
}

// MsgRedelegateExec creates a redelegate message.
func MsgRedelegateExec(clientCtx client.Context, from, src, dst, amount fmt.Stringer,
	extraArgs ...string) (testutil.BufferWriter, error) {

	args := make([]string, 0, 5+len(extraArgs)+len(commonArgs))
	args = append(args,
		src.String(),
		dst.String(),
		amount.String(),
		fmt.Sprintf("--%s=%s", flags.FlagFrom, from.String()),
		fmt.Sprintf("--%s=%d", flags.FlagGas, 300000),
	)
	args = append(args, extraArgs...)
	args = append(args, commonArgs...)
	return clitestutil.ExecTestCLICmd(clientCtx, stakingcli.NewRedelegateCmd(), args)
}

// MsgUnbondExec creates a unbond message.
func MsgUnbondExec(clientCtx client.Context, from fmt.Stringer, valAddress,
	amount fmt.Stringer, extraArgs ...string) (testutil.BufferWriter, error) {

	args := make([]string, 0, 3+len(commonArgs)+len(extraArgs))
	args = append(args,
		valAddress.String(),
		amount.String(),
		fmt.Sprintf("--%s=%s", flags.FlagFrom, from.String()),
	)

	args = append(args, commonArgs...)
	args = append(args, extraArgs...)
	return clitestutil.ExecTestCLICmd(clientCtx, stakingcli.NewUnbondCmd(), args)
}
