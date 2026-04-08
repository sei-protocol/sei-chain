//go:build gofuzz

package tests

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/mempool"
)

func FuzzMempool(f *testing.F) {
	app := kvstore.NewApplication()

	cfg := config.DefaultMempoolConfig()
	cfg.Broadcast = false

	mp := mempool.NewTxMempool(cfg, app)

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = mp.CheckTx(t.Context(), data, nil, mempool.TxInfo{})
	})
}
