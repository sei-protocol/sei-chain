package evmrpc

import (
	"testing"
	"time"
)

func TestRecordRPCMetricsNoPanic(t *testing.T) {
	t.Parallel()
	recordRPCRequest("eth_smoke", "http", true)
	recordRPCLatency("eth_smoke", "http", time.Now().Add(-2*time.Millisecond))
	recordWebsocketConnect()
}
