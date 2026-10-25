package core

import (
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
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

func TestEnvironmentEvmRPCWrappers(t *testing.T) {
	address := common.HexToAddress("0x1000000000000000000000000000000000000001")
	env := &Environment{App: proxy.New(abci.BaseApplication{})}

	// Test: the EVM RPC accessors Environment exposes to giga/evmonly/rpc.
	nonce := env.EvmTransactionCount(address)
	height := env.EvmBlockNumber()
	_, codeErr := env.EvmCode(address)
	_, storageErr := env.EvmStorage(address, common.Hash{})

	// Verify: nonce/height hit Application; the rest error because
	// BaseApplication implements none of those optional interfaces.
	require.Equal(t, uint64(0), nonce)
	require.Equal(t, uint64(0), height)
	require.Error(t, codeErr)
	require.Error(t, storageErr)
}
