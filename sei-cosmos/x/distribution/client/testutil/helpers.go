package testutil

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	distrcli "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/client/cli"
)

func MsgWithdrawDelegatorRewardExec(clientCtx client.Context, valAddr fmt.Stringer, extraArgs ...string) ([]byte, error) {
	buf := new(bytes.Buffer)
	clientCtx = clientCtx.WithOutput(buf)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)

	args := make([]string, 0, 1+len(extraArgs))
	args = append(args, valAddr.String())
	args = append(args, extraArgs...)

	cmd := distrcli.NewWithdrawRewardsCmd()
	cmd.SetErr(buf)
	cmd.SetOut(buf)
	cmd.SetArgs(args)

	if err := cmd.ExecuteContext(ctx); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
