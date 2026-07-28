package evmrpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ctxKey string

// TestBindSubscriptionContextSurvivesParentCancel verifies the eth_subscribe
// per-call RPC ctx is cancelled as soon as the call returns, but the
// bound subscription ctx must keep running (and keep parent values) until the
// subscription err channel closes.
func TestBindSubscriptionContextSurvivesParentCancel(t *testing.T) {
	t.Parallel()

	parent, parentCancel := context.WithCancel(context.WithValue(context.Background(), ctxKey("k"), "v"))
	subErr := make(chan error)
	subCtx, cancel := bindSubscriptionContext(parent, subErr)
	defer cancel()

	parentCancel()
	require.ErrorIs(t, parent.Err(), context.Canceled)
	require.NoError(t, subCtx.Err(), "subscription ctx must outlive per-call cancel")
	require.Equal(t, "v", subCtx.Value(ctxKey("k")), "parent values must be preserved")

	close(subErr)
	require.Eventually(t, func() bool {
		return subCtx.Err() != nil
	}, time.Second, 10*time.Millisecond)
	require.ErrorIs(t, subCtx.Err(), context.Canceled)
}

// TestBindSubscriptionContextCancelUnblocksBinder covers the worker finishing
// before unsubscribe: defer cancel() must let the binder goroutine exit
// without waiting on subErr.
func TestBindSubscriptionContextCancelUnblocksBinder(t *testing.T) {
	t.Parallel()

	subErr := make(chan error) // never closed
	subCtx, cancel := bindSubscriptionContext(context.Background(), subErr)
	cancel()
	require.Eventually(t, func() bool {
		return subCtx.Err() != nil
	}, time.Second, 10*time.Millisecond)
}
