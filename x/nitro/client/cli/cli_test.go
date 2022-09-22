package cli_test

import (
	"fmt"
	"testing"

	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	"github.com/stretchr/testify/require"
	tmcli "github.com/tendermint/tendermint/libs/cli"

	"github.com/sei-protocol/sei-chain/testutil/network"
	"github.com/sei-protocol/sei-chain/x/nitro/client/cli"
	"github.com/sei-protocol/sei-chain/x/nitro/types"
)

func testChain(t *testing.T) *network.Network {
	t.Helper()
	cfg := network.DefaultConfig()
	state := types.GenesisState{}
	require.NoError(t, cfg.Codec.UnmarshalJSON(cfg.GenesisState[types.ModuleName], &state))
	state.Sender = "someone"
	state.Slot = 1
	state.StateRoot = "1234"
	state.Txs = []string{"5678"}
	buf, err := cfg.Codec.MarshalJSON(&state)
	require.NoError(t, err)
	cfg.GenesisState[types.ModuleName] = buf
	return network.New(t, cfg)
}

func TestRecordedTransactionData(t *testing.T) {
	net := testChain(t)

	ctx := net.Validators[0].ClientCtx
	common := []string{
		fmt.Sprintf("--%s=json", tmcli.OutputFlag),
	}
	args := []string{"1"}
	args = append(args, common...)
	out, err := clitestutil.ExecTestCLICmd(ctx, cli.GetCmdRecordedTransactionData(), args)
	require.Nil(t, err)
	var resp types.QueryRecordedTransactionDataResponse
	require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
	require.Equal(t, "5678", resp.Txs[0])
}

func TestSender(t *testing.T) {
	net := testChain(t)

	ctx := net.Validators[0].ClientCtx
	common := []string{
		fmt.Sprintf("--%s=json", tmcli.OutputFlag),
	}
	args := []string{"1"}
	args = append(args, common...)
	out, err := clitestutil.ExecTestCLICmd(ctx, cli.GetCmdSender(), args)
	require.Nil(t, err)
	var resp types.QuerySenderResponse
	require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
	require.Equal(t, "someone", resp.Sender)
}

func TestStateRoot(t *testing.T) {
	net := testChain(t)

	ctx := net.Validators[0].ClientCtx
	common := []string{
		fmt.Sprintf("--%s=json", tmcli.OutputFlag),
	}
	args := []string{"1"}
	args = append(args, common...)
	out, err := clitestutil.ExecTestCLICmd(ctx, cli.GetCmdStateRoot(), args)
	require.Nil(t, err)
	var resp types.QueryStateRootResponse
	require.NoError(t, net.Config.Codec.UnmarshalJSON(out.Bytes(), &resp))
	require.Equal(t, "1234", resp.Root)
}
