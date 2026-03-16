package testutil

import (
	"github.com/sei-protocol/sei-chain/cosmos/testutil"
	clitestutil "github.com/sei-protocol/sei-chain/cosmos/testutil/cli"
	"github.com/sei-protocol/sei-chain/cosmos/testutil/network"
	"github.com/sei-protocol/sei-chain/cosmos/x/authz/client/cli"
)

func ExecGrant(val *network.Validator, args []string) (testutil.BufferWriter, error) {
	cmd := cli.NewCmdGrantAuthorization()
	clientCtx := val.ClientCtx
	return clitestutil.ExecTestCLICmd(clientCtx, cmd, args)
}
