//go:build gofuzz || go1.18

package tests

import (
	"context"
	"testing"

	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/internal/mempool"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

type TestPeerEvictor struct {
	evicting map[types.NodeID]struct{}
}

func NewTestPeerEvictor() *TestPeerEvictor {
	return &TestPeerEvictor{evicting: map[types.NodeID]struct{}{}}
}

func (e *TestPeerEvictor) Evict(id types.NodeID, _ error) {
	e.evicting[id] = struct{}{}
}

func FuzzMempool(f *testing.F) {
	app := kvstore.NewApplication()
	logger := log.NewNopLogger()
	conn := abciclient.NewLocalClient(logger, app)
	err := conn.Start(context.TODO())
	if err != nil {
		panic(err)
	}

	cfg := config.DefaultMempoolConfig()
	cfg.Broadcast = false

	mp := mempool.NewTxMempool(logger, cfg, conn, NewTestPeerEvictor())

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = mp.CheckTx(t.Context(), data, nil, mempool.TxInfo{})
	})
}
