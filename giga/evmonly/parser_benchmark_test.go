package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func BenchmarkPrepareTransferBlock(b *testing.B) {
	const txCount = 5_000
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(b, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xea)
	txs := make([][]byte, txCount)
	preparedByHash := make(map[common.Hash]PreparedTx, txCount)
	for i := range txCount {
		txs[i] = signLegacyTx(b, key, chainID, uint64(i), &recipient, big.NewInt(1), nil)
		tx := new(ethtypes.Transaction)
		require.NoError(b, tx.UnmarshalBinary(txs[i]))
		preparedByHash[tx.Hash()] = PreparedTx{Tx: tx, Sender: sender}
	}
	executor := NewExecutor(Config{})
	request := BlockRequest{Context: blockContext(chainID), Txs: txs}

	b.Run("recover_sender", func(b *testing.B) {
		benchmarkPrepareBlock(b, txCount, func() (PreparedBlock, error) {
			return executor.PrepareBlock(b.Context(), request)
		})
	})
	b.Run("reuse_check_tx", func(b *testing.B) {
		benchmarkPrepareBlock(b, txCount, func() (PreparedBlock, error) {
			return executor.PrepareBlockWithLookup(
				b.Context(),
				request,
				func(hash common.Hash) (PreparedTx, bool) {
					prepared, ok := preparedByHash[hash]
					return prepared, ok
				},
			)
		})
	})
}

func benchmarkPrepareBlock(b *testing.B, txCount int, prepare func() (PreparedBlock, error)) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		prepared, err := prepare()
		if err != nil {
			b.Fatal(err)
		}
		if len(prepared.Txs) != txCount {
			b.Fatalf("prepared %d transactions, want %d", len(prepared.Txs), txCount)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.N*txCount)/b.Elapsed().Seconds(), "tx/s")
}
