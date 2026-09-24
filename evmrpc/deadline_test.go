package evmrpc

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/stretchr/testify/require"
)

func TestWithDeadlineUninstalledEnforcerAppliesNoDeadline(t *testing.T) {
	prev := globalDeadlineEnforcer.Load()
	globalDeadlineEnforcer.Store(nil)

	ctx, cancel := withDeadline(t.Context(), "eth_getBalance")
	defer cancel()
	defer globalDeadlineEnforcer.Store(prev)

	_, ok := ctx.Deadline()
	require.False(t, ok)
}

func TestWithDeadlineAppliesInstalledEnforcerDeadline(t *testing.T) {
	InitGlobalDeadlineEnforcer(ratelimiter.NewDeadlineEnforcer(ratelimiter.DeadlineConfig{
		Default: time.Hour,
		Overrides: map[string]time.Duration{
			"eth_call": time.Minute,
		},
	}))
	defer globalDeadlineEnforcer.Store(nil)

	ctx, cancel := withDeadline(t.Context(), "eth_getBalance")
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Hour), deadline, 5*time.Second)

	ctx, cancel = withDeadline(t.Context(), "eth_call")
	defer cancel()
	deadline, ok = ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Minute), deadline, 5*time.Second)
}

func TestWithDeadlineNeverLengthensAnExistingShorterDeadline(t *testing.T) {
	InitGlobalDeadlineEnforcer(ratelimiter.NewDeadlineEnforcer(ratelimiter.DeadlineConfig{
		Default: time.Hour,
	}))
	defer globalDeadlineEnforcer.Store(nil)

	parent, parentCancel := context.WithTimeout(t.Context(), time.Second)
	defer parentCancel()

	ctx, cancel := withDeadline(parent, "eth_getBalance")
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Second), deadline, 500*time.Millisecond)
}
