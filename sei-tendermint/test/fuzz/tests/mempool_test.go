//go:build gofuzz

package tests

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
)

func FuzzMempool(f *testing.F) {
	cfg := config.DefaultMempoolConfig()
	cfg.Broadcast = false

	mp := mempool.NewTxMempool(cfg.ToMempoolConfig(), kvstore.NewProxy(), mempool.NewMetrics(), mempool.NopTxConstraintsFetcher)

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = mp.CheckTx(t.Context(), data)
	})
}
