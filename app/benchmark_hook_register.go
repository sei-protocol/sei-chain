//go:build benchmark

package app

import (
	"context"
	"os"

	"github.com/sei-protocol/sei-chain/app/benchmark"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
)

func init() {
	server.BenchmarkPostStartHook = registerBenchmarkMempoolPump
}

func registerBenchmarkMempoolPump(ctx context.Context, application abci.Application, lc *local.Local) {
	// Docker localnet sets ID=0..3 on each container; only inject from one node to avoid
	// four independent generators flooding the same mempool.
	if nid := os.Getenv("ID"); nid != "" && nid != "0" {
		return
	}
	seiApp, ok := application.(*App)
	if !ok || seiApp == nil || seiApp.benchmarkManager == nil {
		return
	}
	benchmark.StartMempoolPump(ctx, seiApp.benchmarkManager, lc)
}
