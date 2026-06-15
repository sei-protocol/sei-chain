package mempool

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func BenchmarkCacheInsertTime(b *testing.B) {
	cache := newLRUCache[types.TxHash, struct{}](b.N)

	txs := make([]types.TxHash, b.N)
	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		txs[i] = types.Tx(tx).Hash()
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Push(txs[i], struct{}{})
	}
}

// This benchmark is probably skewed, since we actually will be removing
// txs in parallel, which may cause some overhead due to mutex locking.
func BenchmarkCacheRemoveTime(b *testing.B) {
	cache := newLRUCache[types.TxHash, struct{}](b.N)

	txs := make([]types.TxHash, b.N)
	for i := 0; i < b.N; i++ {
		tx := make([]byte, 8)
		binary.BigEndian.PutUint64(tx, uint64(i))
		txs[i] = types.Tx(tx).Hash()
		cache.Push(txs[i], struct{}{})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cache.Remove(txs[i])
	}
}
