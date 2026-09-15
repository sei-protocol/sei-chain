package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
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
		lookups := 0
		known := func(hash common.Hash) (common.Address, bool) {
			lookups++
			require.Equal(t, tx.Hash(), hash)
			return claimed, true
		}
		prepared, err := parsePreparedTx(rawTx, signer, known)
		require.NoError(t, err)
		require.Equal(t, 1, lookups)
		require.Equal(t, claimed, prepared.Sender)
		require.Equal(t, tx.Hash(), prepared.Tx.Hash())
	})

	t.Run("unknown hash falls back to recovery", func(t *testing.T) {
		known := func(common.Hash) (common.Address, bool) { return claimed, false }
		prepared, err := parsePreparedTx(rawTx, signer, known)
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender)
	})

	t.Run("nil lookup recovers", func(t *testing.T) {
		prepared, err := parsePreparedTx(rawTx, signer, nil)
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender)
	})

	t.Run("sender remembered for other bytes is not applied", func(t *testing.T) {
		otherRaw := signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(1), nil)
		other := decodeTx(t, otherRaw)
		known := func(hash common.Hash) (common.Address, bool) {
			if hash == other.Hash() {
				return claimed, true
			}
			return common.Address{}, false
		}
		prepared, err := parsePreparedTx(rawTx, signer, known)
		require.NoError(t, err)
		require.Equal(t, sender, prepared.Sender)
	})
}

func TestParseBlockTxsMixesKnownAndRecoveredSenders(t *testing.T) {
	chainID := big.NewInt(testChainID)
	signer := ethtypes.LatestSignerForChainID(chainID)
	recipient := testAddress(0xc3)
	const n = 8
	raws := make([][]byte, n)
	senders := make([]common.Address, n)
	known := map[common.Hash]common.Address{}
	for i := range n {
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		raws[i] = signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)
		if i%2 == 0 {
			known[decodeTx(t, raws[i]).Hash()] = senders[i]
		}
	}
	lookup := func(hash common.Hash) (common.Address, bool) {
		sender, ok := known[hash]
		return sender, ok
	}
	for _, workers := range []int{1, 4} {
		parsed, err := parseBlockTxs(t.Context(), raws, signer, lookup, workers)
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
	tx := decodeTx(t, rawTx)
	// A deliberately wrong sender proves the lookup, not recovery, decided.
	claimed := testAddress(0xc5)

	executor := NewExecutor(Config{}, withTestState(NewMemoryState()))
	prepared, err := executor.PrepareBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{rawTx},
		KnownSender: func(hash common.Hash) (common.Address, bool) {
			return claimed, hash == tx.Hash()
		},
	})
	require.NoError(t, err)
	require.Len(t, prepared.Txs, 1)
	require.Equal(t, claimed, prepared.Txs[0].Sender)

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
	known := func(common.Hash) (common.Address, bool) { return sender, true }

	b.Run("recover", func(b *testing.B) {
		for b.Loop() {
			if _, err := parsePreparedTx(rawTx, signer, nil); err != nil {
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
