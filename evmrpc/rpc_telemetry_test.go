package evmrpc

import (
	"testing"
	"time"
)

func TestRecordRPCMetricsNoPanic(t *testing.T) {
	t.Parallel()
	endpoint := "eth_smoke_" + t.Name()
	recordRPCRequest(endpoint, "http", true)
	recordRPCLatency(endpoint, "http", true, time.Now().Add(-2*time.Millisecond))
	recordWebsocketConnect()
	recordFilterLogFetchBatchComplete("logs")
	recordFilterLogFetchBatchComplete("blocks")
}
