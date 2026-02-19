package testutil

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	clitestutil "github.com/sei-protocol/sei-chain/sei-cosmos/testutil/cli"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/network"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz/client/cli"
)

func ExecGrant(val *network.Validator, args []string) (testutil.BufferWriter, error) {
	cmd := cli.NewCmdGrantAuthorization()
	clientCtx := val.ClientCtx
	return clitestutil.ExecTestCLICmd(clientCtx, cmd, args)
}
