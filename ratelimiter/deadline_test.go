package ratelimiter

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestDeadlineEnforcer_Deadline_DefaultAndOverride(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{
		Default: 10 * time.Second,
		Overrides: map[string]time.Duration{
			"eth_call":      30 * time.Second,
			"eth_subscribe": 0,
		},
	})

	require.Equal(t, 10*time.Second, e.Deadline("eth_getBalance"), "unlisted method falls back to Default")
	require.Equal(t, 30*time.Second, e.Deadline("eth_call"), "override takes precedence over Default")
	require.Equal(t, time.Duration(0), e.Deadline("eth_subscribe"), "zero override marks the method deliberately unbounded")
}

func TestDeadlineEnforcer_Deadline_NoDefaultNoOverride(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{})
	require.Equal(t, time.Duration(0), e.Deadline("eth_getBalance"))
}

func TestDeadlineEnforcer_WithDeadline_NoEffectiveDeadlineLeavesCtxUnchanged(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{})
	ctx, cancel := e.WithDeadline(t.Context(), "eth_subscribe")
	defer cancel()
	_, ok := ctx.Deadline()
	require.False(t, ok)
}

func TestDeadlineEnforcer_WithDeadline_AppliesConfiguredDeadline(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{Default: time.Minute})
	ctx, cancel := e.WithDeadline(t.Context(), "eth_getBalance")
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(time.Minute), deadline, time.Second)
}

func TestDeadlineEnforcer_WithDeadline_KeepsAnExistingShorterDeadline(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{Default: time.Minute})
	shortDeadline := time.Now().Add(5 * time.Second)
	parent, parentCancel := context.WithDeadline(t.Context(), shortDeadline)
	defer parentCancel()

	ctx, cancel := e.WithDeadline(parent, "eth_getBalance")
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, shortDeadline, deadline, time.Millisecond)
}

func TestDeadlineEnforcer_WithDeadline_ShortensAnExistingLongerDeadline(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{Default: 5 * time.Second})
	longDeadline := time.Now().Add(time.Hour)
	parent, parentCancel := context.WithDeadline(t.Context(), longDeadline)
	defer parentCancel()

	ctx, cancel := e.WithDeadline(parent, "eth_getBalance")
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)
}

func TestDeadlineEnforcer_WithDeadline_ActuallyExpires(t *testing.T) {
	e := NewDeadlineEnforcer(DeadlineConfig{Default: time.Millisecond})
	ctx, cancel := e.WithDeadline(t.Context(), "eth_getBalance")
	defer cancel()
	<-ctx.Done()
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestDeadlineEnforcer_WithDeadlineAndRecord_IncrementsMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	deadlineMetrics.exceededCounter = must(provider.Meter("ratelimiter").Int64Counter(
		"rpc_deadline_exceeded_total",
	))

	e := NewDeadlineEnforcer(DeadlineConfig{Default: time.Millisecond})
	ctx, cleanup := e.WithDeadlineAndRecord(t.Context(), "evm", "eth_getBalance")
	<-ctx.Done()
	cleanup()

	parent, parentCancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer parentCancel()
	inherited := NewDeadlineEnforcer(DeadlineConfig{Default: time.Hour})
	ctx, cleanup = inherited.WithDeadlineAndRecord(parent, "evm", "eth_getBalance")
	<-ctx.Done()
	cleanup()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.NotEmpty(t, rm.ScopeMetrics)
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "rpc_deadline_exceeded_total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			require.Equal(t, int64(2), sum.DataPoints[0].Value)
			attrs := sum.DataPoints[0].Attributes.ToSlice()
			require.Contains(t, attrs, attribute.String("plane", "evm"))
			require.Contains(t, attrs, attribute.String("method_namespace", "eth"))
			found = true
		}
	}
	require.True(t, found, "expected rpc_deadline_exceeded_total metric")
}
