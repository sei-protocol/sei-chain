package evmrpc

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

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

// withDeadline bounds ctx by method's effective deadline, as resolved by the installed
// DeadlineEnforcer. The returned CancelFunc must always be called. Before
// InitGlobalDeadlineEnforcer runs (for example in tests that construct an API struct
// directly) no enforcer is installed and no deadline is applied.
func withDeadline(ctx context.Context, method string) (context.Context, context.CancelFunc) {
	enforcer := globalDeadlineEnforcer.Load()
	if enforcer == nil {
		return context.WithCancel(ctx)
	}
	return enforcer.WithDeadline(ctx, method)
}

// globalBatchTimeouts holds evmrpc/config.Config.RPCBatchTimeouts, consulted by
// withBatchTimeout.
var globalBatchTimeouts atomic.Pointer[map[string]time.Duration]

// InitGlobalBatchTimeouts installs timeouts as the map withBatchTimeout consults.
func InitGlobalBatchTimeouts(timeouts map[string]time.Duration) {
	globalBatchTimeouts.Store(&timeouts)
}

// withBatchTimeout bounds ctx by method's entry in the installed RPCBatchTimeouts
// map, if any. The returned CancelFunc must always be called. A missing entry, a
// non-positive duration, or no map installed all mean the same thing: no outer
// bound beyond whatever per-call timeouts the method already applies internally.
func withBatchTimeout(ctx context.Context, method string) (context.Context, context.CancelFunc) {
	timeouts := globalBatchTimeouts.Load()
	if timeouts == nil {
		return context.WithCancel(ctx)
	}
	timeout, ok := (*timeouts)[method]
	if !ok || timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
