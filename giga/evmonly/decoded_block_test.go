package evmonly

import (
	"math/big"
	"testing"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestPrepareDecodedBlock(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := testAddress(1)
	raw := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	executor := NewExecutor(Config{ParseWorkers: 2})
	defer executor.Close()
	decoded, err := executor.DecodeBlock(BlockRequest{Context: blockContext(chainID), Txs: [][]byte{raw}})
	require.NoError(t, err)
	require.Equal(t, sender, decoded.txs[0].Sender())
	prepared, err := executor.PrepareDecodedBlock(t.Context(), blockContext(chainID), decoded.txs)
	require.NoError(t, err)
	require.Len(t, prepared.Txs, 1)
	require.Same(t, decoded.txs[0].Transaction(), prepared.Txs[0].Tx)
	require.Equal(t, sender, prepared.Txs[0].Sender)
	decoded, err = executor.DecodeBlock(BlockRequest{Context: blockContext(chainID), Txs: [][]byte{raw}})
	require.NoError(t, err)
	_, err = executor.PrepareDecodedBlock(t.Context(), blockContext(big.NewInt(testChainID+1)), decoded.txs)
	require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	blob := utils.OrPanic1(ethtypes.NewTx(&ethtypes.BlobTx{}).MarshalBinary())
	_, err = executor.DecodeBlock(BlockRequest{Context: blockContext(chainID), Txs: [][]byte{blob}})
	require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
}

func TestDecodeBlockRejectsUnprotectedTransactions(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{}), ethtypes.HomesteadSigner{}, key)
	require.NoError(t, err)
	raw := utils.OrPanic1(tx.MarshalBinary())
	executor := NewExecutor(Config{})
	defer executor.Close()
	decoded, err := executor.DecodeBlock(BlockRequest{Context: blockContext(chainID), Txs: [][]byte{raw}})
	require.Error(t, err)
	require.Empty(t, decoded.txs)
}

func TestPrepareDecodedBlockChecksFork(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.SetCodeTx{ChainID: uint256.MustFromBig(chainID)}), ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw := utils.OrPanic1(tx.MarshalBinary())
	executor := NewExecutor(Config{})
	defer executor.Close()
	decoded, err := executor.DecodeBlock(BlockRequest{Context: blockContext(chainID), Txs: [][]byte{raw}})
	require.NoError(t, err)
	cfg := DefaultChainConfig()
	cfg.PragueTime = nil
	beforePrague := NewExecutor(Config{ChainConfig: cfg})
	defer beforePrague.Close()
	_, err = beforePrague.PrepareDecodedBlock(t.Context(), blockContext(chainID), decoded.txs)
	require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
}
