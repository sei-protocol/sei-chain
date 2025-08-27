package evmrpc_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestSovereignTPSLimiter(t *testing.T) {
	monitor := evmrpc.NewSovereignTPSMonitor(5)

	allowed := 0
	for i := 0; i < 10; i++ {
		if monitor.Allow() {
			allowed++
		}
	}

	require.LessOrEqual(t, allowed, 5, "should not allow more than 5 TPS")
	time.Sleep(time.Second)

	require.True(t, monitor.Allow(), "should allow again after reset")
}
