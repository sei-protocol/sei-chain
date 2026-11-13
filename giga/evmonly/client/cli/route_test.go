package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEVMRPCWithNodeFails(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	err := r.run("", args("q gov proposal 1", "--evm-rpc", stub.url, "--node", "tcp://127.0.0.1:26657")...)
	require.ErrorIs(t, err, errBothEndpoints)
	require.Zero(t, stub.requests)
}

func TestUnroutedCommandFailsByName(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	for _, words := range []string{
		"q gov proposer 1",
		"q upgrade module_versions",
		"q params feesparams",
		"tx gov submit-proposal community-pool-spend proposal.json",
	} {
		for _, extra := range [][]string{nil, {"--evm-rpc", stub.url}} {
			err := r.run("", args(words, extra...)...)
			require.ErrorContains(t, err, "is not supported on an EVM-only chain; use --node", words)
		}
	}
	require.Zero(t, stub.requests)
}

func TestEveryHandlerIsACommand(t *testing.T) {
	r := newTestRoot(t)
	for key := range handlers {
		cmd, _, err := r.cmd.Find(args(key))
		require.NoError(t, err, key)
		require.Equal(t, key, commandKey(cmd))
	}
}

func TestNameWithNodeFails(t *testing.T) {
	r := newTestRoot(t)
	err := r.run("", args("tx gov submit-proposal cancel-software-upgrade --title t --description d --from", testKey, "--name", "v2", "--node", "tcp://127.0.0.1:1")...)
	require.ErrorContains(t, err, "--name cannot be used with --node")
}

func TestWithoutFlagsUsesDefaultEVMRPC(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	block, _, err := r.cmd.Find(args("query block"))
	require.NoError(t, err)
	endpoint := block.PersistentFlags().Lookup(FlagEVMRPC)
	require.Equal(t, DefaultEVMRPC, endpoint.DefValue)
	require.NoError(t, endpoint.Value.Set(stub.url))
	require.NoError(t, r.run("", args("q block 7")...))
	require.Contains(t, r.out.String(), `"number": "0x7"`)
}

func TestCommandGroupPrintsHelp(t *testing.T) {
	r := newTestRoot(t)
	for _, words := range []string{"q gov", "q upgrade", "tx gov"} {
		require.NoError(t, r.run("", args(words)...), words)
		require.Contains(t, r.out.String(), "Available Commands", words)
	}
}

func TestNodeRunsV2(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	err := r.run("", args("q gov proposal 1 --node tcp://127.0.0.1:1")...)
	require.Error(t, err)
	require.NotErrorIs(t, err, errBothEndpoints)
	require.Zero(t, stub.requests)
}

func TestNonEVMOnlyChainIsRefused(t *testing.T) {
	stub := newRPCStub(t)
	stub.on("param(string,string)", func([]any) ([]any, error) { return nil, errors.New("execution reverted") })
	for _, words := range []string{"q gov proposal 1", "q upgrade plan", "tx gov vote 1 yes --from " + testKey} {
		err := newTestRoot(t).run("", args(words, "--evm-rpc", stub.url)...)
		require.ErrorContains(t, err, stub.url+" is not an EVM-only chain; use --node", words)
	}
	require.Empty(t, stub.sent)
}

func TestUnreachableEndpointIsNotReportedAsV2(t *testing.T) {
	err := newTestRoot(t).run("", args("q gov proposal 1", "--evm-rpc", "http://127.0.0.1:1")...)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "not an EVM-only chain")
}
