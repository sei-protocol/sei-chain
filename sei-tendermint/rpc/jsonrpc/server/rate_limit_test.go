package server

import (
	"testing"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/stretchr/testify/require"
)

func TestRateLimitGate_CheckCatalog_RejectsAfterBurst(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	ip := "203.0.113.50"

	allowed, rejectMethod := gate.CheckCatalog(t.Context(), ip)
	require.True(t, allowed)
	require.Empty(t, rejectMethod)

	allowed, rejectMethod = gate.CheckCatalog(t.Context(), ip)
	require.False(t, allowed)
	require.Equal(t, cometbftMethodCatalog, rejectMethod)
}

func TestRateLimitGate_CheckURI_InvalidPathReturns400(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true)

	allowed, rejectMethod, err := gate.CheckURI(t.Context(), "203.0.113.1", "/")
	require.False(t, allowed)
	require.Empty(t, rejectMethod)
	require.ErrorIs(t, err, errInvalidURIMethod)
}

func TestRateLimitGate_CheckURI_InvalidPathReturns429WhenBucketExhausted(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	ip := "203.0.113.50"

	allowed, rejectMethod, err := gate.CheckURI(t.Context(), ip, "/status")
	require.True(t, allowed)
	require.NoError(t, err)
	require.Empty(t, rejectMethod)

	allowed, rejectMethod, err = gate.CheckURI(t.Context(), ip, "/status")
	require.False(t, allowed)
	require.Equal(t, "status", rejectMethod)
	require.NoError(t, err)

	allowed, rejectMethod, err = gate.CheckURI(t.Context(), ip, "/")
	require.False(t, allowed)
	require.Equal(t, ratelimiter.MethodInvalid, rejectMethod)
	require.NoError(t, err)
}
