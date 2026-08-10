package evmrpc

import (
	"context"
	"sync"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type historicalTraceErrorCollector struct {
	mu  sync.Mutex
	err error
}

func (c *historicalTraceErrorCollector) RecordHistoricalTraceError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
	}
}

func (c *historicalTraceErrorCollector) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

type historicalTraceErrorCollectorKey struct{}

func withHistoricalTraceErrorCollector(ctx context.Context) (context.Context, *historicalTraceErrorCollector) {
	collector := &historicalTraceErrorCollector{}
	return context.WithValue(ctx, historicalTraceErrorCollectorKey{}, collector), collector
}

func historicalTraceErrorCollectorFromContext(ctx context.Context) *historicalTraceErrorCollector {
	collector, _ := ctx.Value(historicalTraceErrorCollectorKey{}).(*historicalTraceErrorCollector)
	return collector
}

// attachHistoricalTraceErrorCollector bridges the RPC request context into the
// SDK context used by precompiles during replay.
func attachHistoricalTraceErrorCollector(sdkCtx sdk.Context, requestCtx context.Context) sdk.Context {
	collector := historicalTraceErrorCollectorFromContext(requestCtx)
	if collector == nil {
		return sdkCtx
	}
	return pcommon.WithHistoricalTraceErrorRecorder(sdkCtx, collector)
}

func rejectUnavailableHistoricalTrace(result interface{}, traceErr error, collector *historicalTraceErrorCollector) (interface{}, error) {
	if err := collector.Err(); err != nil {
		return nil, err
	}
	return result, traceErr
}
