package rpcadmission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testPool(t *testing.T, mutate func(*Config)) *Pool {
	t.Helper()
	cfg := Config{}
	mutate(&cfg)
	pool, err := NewPool(cfg)
	require.NoError(t, err)
	return pool
}

func TestPoolEnforcesGlobalWeightedCapacity(t *testing.T) {
	pool := testPool(t, func(cfg *Config) {
		cfg.GlobalLimit = 20
		cfg.ClassTimeouts.Trace = 0
		cfg.ClassTimeouts.CheapRead = 0
	})

	trace, err := pool.AcquireMethod(t.Context(), "debug_traceTransaction")
	require.NoError(t, err)
	_, err = pool.AcquireMethod(t.Context(), "eth_chainId")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	trace.Release()
	cheap, err := pool.AcquireMethod(t.Context(), "eth_chainId")
	require.NoError(t, err)
	cheap.Release()
}

func TestPoolEnforcesClassCapacityIndependently(t *testing.T) {
	pool := testPool(t, func(cfg *Config) {
		cfg.GlobalLimit = 100
		cfg.ClassLimits.EVMExecution = 10
		cfg.ClassTimeouts.EVMExecution = 0
		cfg.ClassTimeouts.CheapRead = 0
	})

	execution, err := pool.AcquireMethod(t.Context(), "eth_call")
	require.NoError(t, err)
	_, err = pool.AcquireMethod(t.Context(), "eth_estimateGas")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	cheap, err := pool.AcquireMethod(t.Context(), "eth_chainId")
	require.NoError(t, err)
	cheap.Release()
	execution.Release()
}

func TestPoolWaitsForReleasedCapacity(t *testing.T) {
	pool := testPool(t, func(cfg *Config) {
		cfg.GlobalLimit = 20
		cfg.ClassTimeouts.Trace = time.Second
	})

	first, err := pool.AcquireMethod(t.Context(), "debug_traceCall")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		second, acquireErr := pool.AcquireMethod(t.Context(), "debug_traceTransaction")
		if acquireErr == nil {
			second.Release()
		}
		done <- acquireErr
	}()

	first.Release()
	require.NoError(t, <-done)
}

func TestPermitReleaseIsIdempotent(t *testing.T) {
	pool := testPool(t, func(cfg *Config) {
		cfg.GlobalLimit = 20
		cfg.ClassTimeouts.Trace = 0
	})
	permit, err := pool.AcquireMethod(t.Context(), "debug_traceCall")
	require.NoError(t, err)
	permit.Release()
	permit.Release()

	next, err := pool.AcquireMethod(t.Context(), "debug_traceCall")
	require.NoError(t, err)
	next.Release()
	(&Permit{}).Release()
}

func TestPoolReleasesClassCapacityWhenGlobalAdmissionFails(t *testing.T) {
	pool := testPool(t, func(cfg *Config) {
		cfg.GlobalLimit = ClassTrace.Weight()
		cfg.ClassLimits.EVMExecution = ClassEVMExecution.Weight()
		cfg.ClassTimeouts.Trace = 0
		cfg.ClassTimeouts.EVMExecution = 0
	})

	trace, err := pool.AcquireMethod(t.Context(), "debug_traceCall")
	require.NoError(t, err)
	_, err = pool.AcquireMethod(t.Context(), "eth_call")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	trace.Release()

	execution, err := pool.AcquireMethod(t.Context(), "eth_call")
	require.NoError(t, err)
	execution.Release()
}

func TestPoolHonorsCallerCancellation(t *testing.T) {
	pool := testPool(t, func(cfg *Config) { cfg.GlobalLimit = 100 })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := pool.AcquireMethod(ctx, "eth_chainId")
	require.True(t, errors.Is(err, context.Canceled))
}

func TestPoolTreatsUnknownClassAsNormalRead(t *testing.T) {
	pool := testPool(t, func(cfg *Config) {
		cfg.ClassLimits.NormalRead = ClassNormalRead.Weight()
		cfg.ClassTimeouts.NormalRead = 0
	})
	first, err := pool.Acquire(t.Context(), MethodClass("unknown"))
	require.NoError(t, err)
	_, err = pool.Acquire(t.Context(), MethodClass("unknown"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	first.Release()
}
