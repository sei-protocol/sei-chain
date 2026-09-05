package core

import (
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestBroadcastTxCommitUnderAutobahnFailsFast(t *testing.T) {
	// Setup: Giga validator RPC env with a real mempool and an empty KV indexer.
	env := newAutobahnBroadcastEnv(t)

	// Test: BroadcastTxCommit with the live RPC defaults.
	res, err := env.BroadcastTxCommit(t.Context(), &coretypes.RequestBroadcastTx{Tx: []byte("tx")})

	// Verify: Autobahn sentinel and no result.
	require.ErrorIs(t, err, ErrBroadcastTxCommitUnsupported)
	require.Nil(t, res)
}

func TestBroadcastTxCommitWithoutAutobahnUsesMempool(t *testing.T) {
	// Setup: RPC env with no GigaRouter (Comet path).
	env := &Environment{}

	// Test: BroadcastTxCommit with no local mempool either.
	_, err := env.BroadcastTxCommit(t.Context(), &coretypes.RequestBroadcastTx{Tx: []byte("tx")})

	// Verify: mempool error, not the Autobahn sentinel.
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrBroadcastTxCommitUnsupported))
}
