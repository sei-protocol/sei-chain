package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyQueryDefaultsReplacesZeroMaxCaps(t *testing.T) {
	cfg := ApplyQueryDefaults(QueryConfig{
		MaxLimit:      0,
		MaxOffset:     0,
		MaxIterations: 0,
	})

	require.Equal(t, DefaultQueryMaxLimit, cfg.MaxLimit)
	require.Equal(t, DefaultQueryMaxOffset, cfg.MaxOffset)
	require.Equal(t, DefaultQueryMaxIterations, cfg.MaxIterations)
}

func TestApplyQueryDefaultsPreservesNonZeroMaxCaps(t *testing.T) {
	cfg := ApplyQueryDefaults(QueryConfig{
		MaxLimit:      500,
		MaxOffset:     5_000,
		MaxIterations: 6_000,
	})

	require.Equal(t, uint64(500), cfg.MaxLimit)
	require.Equal(t, uint64(5_000), cfg.MaxOffset)
	require.Equal(t, uint64(6_000), cfg.MaxIterations)
}

func TestApplyQueryDefaultsReplacesOnlyZeroFields(t *testing.T) {
	cfg := ApplyQueryDefaults(QueryConfig{
		MaxLimit:      500,
		MaxOffset:     0,
		MaxIterations: 6_000,
	})

	require.Equal(t, uint64(500), cfg.MaxLimit)
	require.Equal(t, DefaultQueryMaxOffset, cfg.MaxOffset)
	require.Equal(t, uint64(6_000), cfg.MaxIterations)
}
