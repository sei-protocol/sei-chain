package cli

import (
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
		err := r.run("", args(words, "--evm-rpc", stub.url)...)
		require.ErrorContains(t, err, "is not supported with --evm-rpc", words)
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

func TestNameNeedsEVMRPC(t *testing.T) {
	r := newTestRoot(t)
	err := r.run("", args("tx gov submit-proposal cancel-software-upgrade --title t --description d --from", testKey, "--name", "v2")...)
	require.ErrorContains(t, err, "--name needs --evm-rpc")
}

func TestWithoutEVMRPCRunsV2(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	err := r.run("", args("q gov proposal 1 --node tcp://127.0.0.1:1")...)
	require.Error(t, err)
	require.NotErrorIs(t, err, errBothEndpoints)
	require.Zero(t, stub.requests)
}
