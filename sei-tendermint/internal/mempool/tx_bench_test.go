package mempool

import (
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func benchTxStore(b *testing.B, numAccounts, txsPerAccount int) *txStore {
	rng := utils.TestRng()
	app := newEVMNonceApp()
	cfg := TestConfig()
	cfg.Size = numAccounts * txsPerAccount
	cfg.PendingSize = numAccounts * txsPerAccount
	cfg.MaxTxsBytes = 1 << 40
	cfg.MaxPendingTxsBytes = 1 << 40
	store := NewTxStore(cfg, proxy.New(app))
	for range numAccounts {
		addr := genEvmAddress(rng)
		app.setNonce(addr, 0)
		app.setBalance(addr, 1<<30)
		for n := range txsPerAccount {
			wtx := makeEvmTxForTest(rng, addr, uint64(n), int64(rng.Intn(1000)), 1)
			require.NoError(b, store.Insert(wtx))
		}
	}
	return store
}

func BenchmarkTxStore_Update(b *testing.B) {
	for _, tc := range []struct{ accounts, per int }{{10000, 1}, {5000, 4}, {50000, 1}} {
		b.Run(fmt.Sprintf("accounts=%d,per=%d", tc.accounts, tc.per), func(b *testing.B) {
			store := benchTxStore(b, tc.accounts, tc.per)
			b.ResetTimer()
			for range b.N {
				store.Update(updateSpec{Now: time.Now(), Height: 1, Constraints: TxConstraints{MaxGas: -1}})
			}
		})
	}
}

func BenchmarkTxStore_InInclusionOrder(b *testing.B) {
	for _, tc := range []struct{ accounts, per int }{{10000, 1}, {5000, 4}, {50000, 1}} {
		b.Run(fmt.Sprintf("accounts=%d,per=%d", tc.accounts, tc.per), func(b *testing.B) {
			store := benchTxStore(b, tc.accounts, tc.per)
			b.ResetTimer()
			for range b.N {
				for inner := range store.inner.Lock() {
					inner.inInclusionOrder()
				}
			}
		})
	}
}

func BenchmarkTxStore_ReapRemove(b *testing.B) {
	for _, tc := range []struct{ accounts, per int }{{10000, 1}, {5000, 4}} {
		b.Run(fmt.Sprintf("accounts=%d,per=%d", tc.accounts, tc.per), func(b *testing.B) {
			for range b.N {
				b.StopTimer()
				store := benchTxStore(b, tc.accounts, tc.per)
				b.StartTimer()
				store.Reap(ReapLimits{MaxTxs: utils.Some(uint64(100))}, true)
			}
		})
	}
}
