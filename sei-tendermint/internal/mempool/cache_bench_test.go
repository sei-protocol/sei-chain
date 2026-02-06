package mempool

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func BenchmarkCacheInsertTime(b *testing.B) {
	cache := NewLRUTxCache(b.N, 0)

	txs := make([]types.TxKey, b.N)
	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		txs[i] = types.Tx(tx).Key()
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Push(txs[i])
	}
}

// This benchmark is probably skewed, since we actually will be removing
// txs in parallel, which may cause some overhead due to mutex locking.
func BenchmarkCacheRemoveTime(b *testing.B) {
	cache := NewLRUTxCache(b.N, 0)

	txs := make([]types.TxKey, b.N)
	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		txs[i] = types.Tx(tx).Key()
		cache.Push(txs[i])
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Remove(txs[i])
	}
}
