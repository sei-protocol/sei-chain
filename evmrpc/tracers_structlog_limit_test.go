package evmrpc

import (
	"math"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	traceLogger "github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/stretchr/testify/require"
)

func TestClampUint64ToInt(t *testing.T) {
	require.Equal(t, 0, clampUint64ToInt(0))
	require.Equal(t, 1024, clampUint64ToInt(1024))
	require.Equal(t, math.MaxInt, clampUint64ToInt(math.MaxInt))
	require.Equal(t, math.MaxInt, clampUint64ToInt(math.MaxInt+1), "values above MaxInt must saturate, not wrap negative")
	require.Equal(t, math.MaxInt, clampUint64ToInt(math.MaxUint64))
}

func TestClampDefaultStructLogLimit(t *testing.T) {
	const capacity = 256 * 1024 * 1024

	t.Run("imposes capacity when caller sets no limit", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: capacity}
		cfg := &tracers.TraceConfig{}
		api.clampDefaultStructLogLimit(cfg)
		require.NotNil(t, cfg.Config)
		require.Equal(t, capacity, cfg.Config.Limit)
	})

	t.Run("imposes capacity when nested Config is nil", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: capacity}
		cfg := &tracers.TraceConfig{Config: nil}
		api.clampDefaultStructLogLimit(cfg)
		require.NotNil(t, cfg.Config)
		require.Equal(t, capacity, cfg.Config.Limit)
	})

	t.Run("clamps a larger caller-supplied limit down to the capacity", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: capacity}
		cfg := &tracers.TraceConfig{Config: &traceLogger.Config{Limit: capacity * 4}}
		api.clampDefaultStructLogLimit(cfg)
		require.Equal(t, capacity, cfg.Config.Limit)
	})

	t.Run("honors a smaller caller-supplied limit", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: capacity}
		cfg := &tracers.TraceConfig{Config: &traceLogger.Config{Limit: 1024}}
		api.clampDefaultStructLogLimit(cfg)
		require.Equal(t, 1024, cfg.Config.Limit)
	})

	t.Run("no-op for custom tracers", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: capacity}
		name := callTracerName
		cfg := &tracers.TraceConfig{Tracer: &name}
		api.clampDefaultStructLogLimit(cfg)
		require.Nil(t, cfg.Config, "custom tracers must not be given a struct-logger limit")
	})

	t.Run("no-op when capacity is disabled", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: 0}
		cfg := &tracers.TraceConfig{}
		api.clampDefaultStructLogLimit(cfg)
		require.Nil(t, cfg.Config, "disabled capacity (0) must preserve upstream unlimited behavior")
	})

	t.Run("nil config is safe", func(t *testing.T) {
		api := &DebugAPI{maxStructLogBytes: capacity}
		require.NotPanics(t, func() { api.clampDefaultStructLogLimit(nil) })
	})
}
