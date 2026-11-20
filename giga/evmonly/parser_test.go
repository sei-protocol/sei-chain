package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestExecutorPrepareBlockResolvesSender(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xc4)
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)

	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	require.Len(t, prepared.Txs, 1)
	require.Equal(t, sender, prepared.Txs[0].Sender)

	wrongChain := signLegacyTx(t, key, big.NewInt(testChainID+1), 0, &recipient, big.NewInt(1), nil)
	_, err = executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{wrongChain},
	})
	require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)

	prepared, err = executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	require.Equal(t, sender, prepared.Txs[0].Sender)
}

func TestExecutorDecodeResolvesSenders(t *testing.T) {
	chainID := big.NewInt(testChainID)
	recipient := testAddress(0xc7)
	const n = 40
	raws := make([][]byte, n)
	senders := make([]common.Address, n)
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, uint64(i), &recipient, big.NewInt(1), nil) //nolint:gosec // G115: i < n.
	}
	req := BlockRequest{Context: blockContext(chainID), Txs: raws}
	executor := NewExecutor(Config{ParseWorkers: 4}, withTestState(NewMemoryState()))

	decoded, err := executor.DecodeBlock(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, decoded.txs, n)
	for i, tx := range decoded.txs {
		require.Equal(t, senders[i], tx.Sender())
	}
	recovered, err := executor.PrepareDecodedBlock(t.Context(), decoded.context, decoded.txs)
	require.NoError(t, err)
	prepared, err := executor.PrepareBlock(t.Context(), req)
	require.NoError(t, err)

	require.Equal(t, prepared.Context, recovered.Context)
	require.Len(t, recovered.Txs, n)
	for i := range n {
		require.Equal(t, senders[i], recovered.Txs[i].Sender)
		require.Equal(t, prepared.Txs[i].Sender, recovered.Txs[i].Sender)
		require.Equal(t, prepared.Txs[i].Tx.Hash(), recovered.Txs[i].Tx.Hash())
	}

	_, err = executor.DecodeBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: [][]byte{{0x01}}})
	require.Error(t, err)
}
