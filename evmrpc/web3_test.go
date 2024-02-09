package evmrpc_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestClientVersion(t *testing.T) {
	w := evmrpc.Web3API{}
	require.NotEmpty(t, w.ClientVersion())
}
