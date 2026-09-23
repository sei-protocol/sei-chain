package ratelimiter

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// DeadlineConfig configures a DeadlineEnforcer.
type DeadlineConfig struct {
	// Default is the deadline applied to a method with no entry in Overrides.
	// Zero means no deadline is applied by default.
	Default time.Duration
	// Overrides maps a method name to its own deadline, taking precedence over
	// Default. A zero entry marks that method as deliberately unbounded (for
	// example a long-poll subscription), overriding Default for it.
	Overrides map[string]time.Duration
}

// DeadlineEnforcer resolves and applies configured request deadlines for RPC methods.
type DeadlineEnforcer struct {
	cfg DeadlineConfig
}

// NewDeadlineEnforcer returns a DeadlineEnforcer for cfg. A nil cfg.Overrides is
// treated as empty.
func NewDeadlineEnforcer(cfg DeadlineConfig) *DeadlineEnforcer {
	return &DeadlineEnforcer{cfg: cfg}
}

// Deadline returns the override for method when present and Default otherwise.
// Zero means no deadline should be applied.
func (e *DeadlineEnforcer) Deadline(method string) time.Duration {
	if override, ok := e.cfg.Overrides[method]; ok {
		return override
	}
	return e.cfg.Default
}

// WithDeadline returns a context bounded by the effective deadline for method,
// honoring any deadline ctx already carries: the shorter of the two wins, so a
// caller-supplied deadline (for example gRPC's grpc-timeout, which grpc-go
// applies to ctx before a handler ever sees it) is never lengthened. When the
// effective deadline is zero and ctx carries none, ctx is returned unchanged.
// The returned CancelFunc must be called in every case to release resources.
func (e *DeadlineEnforcer) WithDeadline(ctx context.Context, method string) (context.Context, context.CancelFunc) {
	d := e.Deadline(method)
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	deadline := time.Now().Add(d)
	if existing, ok := ctx.Deadline(); ok && existing.Before(deadline) {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline)
}

// RecordExceeded increments rpc_deadline_exceeded_total{plane, method_namespace}.
// Callers invoke it once per request, after the handler returns, when ctx.Err()
// is context.DeadlineExceeded; RecordExceeded does not inspect ctx itself, so
// distinguishing that from a client-side context.Canceled is the caller's
// responsibility.
func (e *DeadlineEnforcer) RecordExceeded(ctx context.Context, plane, method string) {
	deadlineMetrics.exceededCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("plane", plane),
			attribute.String("method_namespace", bucketRPCMethod(plane, method, nil)),
		),
	)
}
