package server

import (
	"context"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client/local"
)

// BenchmarkPostStartHook runs after the in-process Comet node is started. When non-nil,
// a local RPC client is created (even if API/gRPC are disabled) so the hook can
// inject work (e.g. benchmark txs into the mempool). Sei-chain registers this when
// built with the "benchmark" build tag.
var BenchmarkPostStartHook func(ctx context.Context, application abci.Application, localClient *local.Local)
