package config

import (
	"strings"
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

func TestValidateQueryConfigNoWarningsForDefaults(t *testing.T) {
	require.Empty(t, ValidateQueryConfig(DefaultQueryConfig()))
}

func TestValidateQueryConfigWarnsWhenMaxIterationsBelowOffsetPlusLimit(t *testing.T) {
	warnings := ValidateQueryConfig(QueryConfig{
		MaxLimit:      1_000,
		MaxOffset:     10_000,
		MaxIterations: 10_000,
	})
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "query.max-iterations (10000) is below query.max-offset + query.max-limit (11000)")
}

func TestValidateQueryConfigSkipsCapWarningWhenLimitsDisabled(t *testing.T) {
	warnings := ValidateQueryConfig(QueryConfig{
		DisableLimits: true,
		MaxLimit:      1_000,
		MaxOffset:     10_000,
		MaxIterations: 1,
	})
	require.Empty(t, warnings)
}

func TestValidateQueryConfigWarnsInvalidTrustedCIDR(t *testing.T) {
	warnings := ValidateQueryConfig(QueryConfig{
		TrustedCIDRs: []string{"not-a-cidr"},
	})
	require.Len(t, warnings, 1)
	require.True(t, strings.Contains(warnings[0], "invalid CIDR"))
}
