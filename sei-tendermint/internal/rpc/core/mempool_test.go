package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func TestBroadcastTxCommitWithoutAutobahnUsesMempool(t *testing.T) {
	// Setup: RPC env with no GigaRouter (Comet path).
	env := &Environment{}

	// Test: BroadcastTxCommit with no local mempool either.
	_, err := env.BroadcastTxCommit(t.Context(), &coretypes.RequestBroadcastTx{Tx: []byte("tx")})

	// Verify: the missing mempool returns an error.
	require.Error(t, err)
}

func TestEvmPendingNonceFollowsMempool(t *testing.T) {
	address := common.HexToAddress("0x1000000000000000000000000000000000000001")

	t.Run("fullnode", func(t *testing.T) {
		env := newAutobahnFullnodeEnv(t)
		_, ok := env.EvmPendingNonce(address).Get()
		require.False(t, ok)
	})

	t.Run("validator", func(t *testing.T) {
		env := newAutobahnBroadcastEnv(t)
		giga := env.Router
		mp, ok := giga.Mempool().Get()
		require.True(t, ok)
		got, ok := env.EvmPendingNonce(address).Get()
		require.True(t, ok)
		require.Equal(t, mp.EvmNextPendingNonce(address), got)
	})
}
