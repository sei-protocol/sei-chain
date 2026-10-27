package ratelimiter

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustGateRegistry(t *testing.T, rps float64, burst int) *Registry {
	t.Helper()
	reg, err := New(Config{RPS: rps, Burst: burst})
	require.NoError(t, err)
	return reg
}

func TestGate_CheckJSONRPC(t *testing.T) {
	reg := mustGateRegistry(t, 0.001, 1)
	gate := NewGate(reg, "test", 0)

	allowed, _, err := gate.CheckJSONRPC(t.Context(), "1.2.3.4", strings.NewReader(
		`{"method":"eth_call","id":1}`,
	))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, rejectMethod, err := gate.CheckJSONRPC(t.Context(), "1.2.3.4", strings.NewReader(
		`{"method":"eth_getBalance","id":2}`,
	))
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "eth_getBalance", rejectMethod)
}

func TestGate_CheckJSONRPCBatchReportsRejectedBatchLen(t *testing.T) {
	reg := mustGateRegistry(t, 0.001, 1)
	gate := NewGate(reg, "test", 0)

	allowed, rejectMethod, batchLen, err := gate.CheckJSONRPCBatch(t.Context(), "1.2.3.4", strings.NewReader(
		`[{"method":"eth_call","id":1},{"method":"eth_getBalance","id":2}]`,
	))
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "eth_call", rejectMethod)
	require.Equal(t, 2, batchLen)
}

func TestGate_CheckJSONRPCBatchReportsMalformedBatchAsZero(t *testing.T) {
	reg := mustGateRegistry(t, 0.001, 1)
	gate := NewGate(reg, "test", 0)
	ip := "1.2.3.4"
	require.True(t, reg.Allow(t.Context(), ip, "test", "prime"))

	allowed, rejectMethod, batchLen, err := gate.CheckJSONRPCBatch(t.Context(), ip, strings.NewReader(`[{}]`))
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, MethodInvalid, rejectMethod)
	require.Zero(t, batchLen)
}

func TestGate_CheckJSONRPC_ProbeLimitRejected(t *testing.T) {
	reg := mustGateRegistry(t, 100, 10)
	gate := NewGate(reg, "test", 64)

	padding := strings.Repeat(" ", 50)
	body := `{"params":[` + padding + `],"method":"eth_call","id":1}`

	allowed, rejectMethod, err := gate.CheckJSONRPC(t.Context(), "1.2.3.4", strings.NewReader(body))
	require.ErrorIs(t, err, ErrProbeLimit)
	require.False(t, allowed)
	require.Empty(t, rejectMethod)
}

func TestGate_ChargeAdmissionRejection(t *testing.T) {
	reg := mustGateRegistry(t, 0.001, 1)
	gate := NewGate(reg, "test", 0)
	ip := "203.0.113.50"

	require.False(t, gate.ChargeAdmissionRejection(t.Context(), ip))
	require.True(t, gate.ChargeAdmissionRejection(t.Context(), ip))
}

func TestNewGate_MaxInt64BodyLimitClamped(t *testing.T) {
	reg := mustGateRegistry(t, 100, 10)
	gate := NewGate(reg, "test", math.MaxInt64)
	require.Equal(t, int64(math.MaxInt64-1), gate.MaxBodyBytes())
}
