package evmrpc

import (
	"context"
	"testing"
	"time"
)

func TestRecordRPCMetricsNoPanic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	endpoint := "eth_smoke_" + t.Name()
	recordRPCRequest(ctx, endpoint, "http", true)
	recordRPCLatency(ctx, endpoint, "http", true, time.Now().Add(-2*time.Millisecond))
	recordWebsocketConnect(ctx)
	recordFilterLogFetchBatchComplete(ctx, "logs")
	recordFilterLogFetchBatchComplete(ctx, "blocks")
}
