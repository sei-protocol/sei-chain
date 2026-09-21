package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestParsePreparedTxUsesKnownSender(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xc1)
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	tx := decodeTx(t, rawTx)
	claimed := testAddress(0xc2)

	t.Run("known sender is used without recovery", func(t *testing.T) {
		prepared, err := parsePreparedTx(rawTx, signer, utils.Some(claimed))
		require.NoError(t, err)
		require.Equal(t, claimed, prepared.Sender)
		require.Equal(t, tx.Hash(), prepared.Tx.Hash())
	})

	t.Run("no known sender recovers", func(t *testing.T) {
		prepared, err := parsePreparedTx(rawTx, signer, utils.None[common.Address]())
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender)
	})

	t.Run("known sender is ignored for a tx from another chain", func(t *testing.T) {
		otherChain := big.NewInt(testChainID + 1)
		otherRaw := signLegacyTx(t, key, otherChain, 0, &recipient, big.NewInt(1), nil)
		_, err := parsePreparedTx(otherRaw, signer, utils.Some(claimed))
		require.ErrorIs(t, err, ethtypes.ErrInvalidChainId)
	})

	t.Run("known sender is ignored for a type the signer does not support", func(t *testing.T) {
		legacyOnly := ethtypes.NewEIP155Signer(chainID)
		dynamicRaw := signDynamicFeeTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		_, err := parsePreparedTx(dynamicRaw, legacyOnly, utils.Some(claimed))
		require.ErrorIs(t, err, ethtypes.ErrTxTypeNotSupported)
	})
}

func TestParseBlockTxsMixesKnownAndRecoveredSenders(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	recipient := testAddress(0xc3)
	const n = 8
	raws := make([][]byte, n)
	senders := make([]common.Address, n)
	known := make([]utils.Option[common.Address], n)
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		if i%2 == 0 {
			known[i] = utils.Some(senders[i])
		}
	}
	for _, workers := range []int{1, 4} {
		parsed, err := parseBlockTxs(t.Context(), raws, signer, known, workers)
		require.NoError(t, err)
		require.Len(t, parsed, n)
		for i, prepared := range parsed {
			require.Equal(t, senders[i], prepared.Sender)
		}
	}
}

func TestExecutorPrepareBlockUsesKnownSender(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xc4)
	rawTx := signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
	// A deliberately wrong sender proves the slice, not recovery, decided.
	claimed := testAddress(0xc5)

	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
		Senders: []utils.Option[common.Address]{utils.Some(claimed)},
	})
	require.NoError(t, err)
	require.Len(t, prepared.Txs, 1)
	require.Equal(t, claimed, prepared.Txs[0].Sender)

	_, err = executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx, rawTx},
		Senders: []utils.Option[common.Address]{utils.Some(claimed)},
	})
	require.Error(t, err)

	prepared, err = executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
	})
	require.NoError(t, err)
	require.Equal(t, sender, prepared.Txs[0].Sender)
}

func BenchmarkParsePreparedTx(b *testing.B) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	key, err := crypto.GenerateKey()
	require.NoError(b, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xc6)
	rawTx := signLegacyTx(b, key, chainID, 0, &recipient, big.NewInt(1), nil)
	known := utils.Some(sender)

	b.Run("recover", func(b *testing.B) {
		for b.Loop() {
			if _, err := parsePreparedTx(rawTx, signer, utils.None[common.Address]()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("known", func(b *testing.B) {
		for b.Loop() {
			if _, err := parsePreparedTx(rawTx, signer, known); err != nil {
				b.Fatal(err)
			}
		}
	})
}
