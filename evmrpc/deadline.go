package evmrpc

import (
	"context"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

const evmDeadlinePlane = "evm"

// globalDeadlineEnforcer is installed by InitGlobalDeadlineEnforcer and consulted by
// withDeadline. It is package-level, not threaded through every API struct, because it
// applies uniformly across every registered API and both the HTTP and WebSocket servers
// share one evmrpc/config.Config; see InitGlobalWorkerPool for the same pattern.
var globalDeadlineEnforcer atomic.Pointer[ratelimiter.DeadlineEnforcer]

// InitGlobalDeadlineEnforcer installs enforcer as the DeadlineEnforcer used by withDeadline.
// Called by both NewEVMHTTPServer and NewEVMWebSocketServer; both build enforcer from the
// same evmrpc/config.Config, so a second call is a no-op in practice, not a race.
func InitGlobalDeadlineEnforcer(enforcer *ratelimiter.DeadlineEnforcer) {
	globalDeadlineEnforcer.Store(enforcer)
}

// withDeadline bounds ctx by method's effective deadline and records when it is
// exceeded. The returned CancelFunc must always be called. Before
// InitGlobalDeadlineEnforcer runs, no deadline is applied or recorded.
func withDeadline(ctx context.Context, method string) (context.Context, context.CancelFunc) {
	enforcer := globalDeadlineEnforcer.Load()
	if enforcer == nil {
		return context.WithCancel(ctx)
	}
	return enforcer.WithDeadlineAndRecord(ctx, evmDeadlinePlane, method)
}
