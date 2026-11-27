package rpc

import (
	"testing"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

// requireErrorCode requires err to carry the JSON-RPC error code code as go-ethereum's server encodes
// it: from err itself, never from an error err wraps. An error a client received carries the code
// that came over the wire.
func requireErrorCode(t *testing.T, err error, code int) {
	t.Helper()
	require.Error(t, err)
	// A type assertion, not errors.As: the server reads only the outermost error's code.
	rpcErr, ok := err.(ethrpc.Error)
	require.Truef(t, ok, "%T carries no JSON-RPC error code at its outermost level: %v", err, err)
	require.Equalf(t, code, rpcErr.ErrorCode(), "error code of %q", err)
}
