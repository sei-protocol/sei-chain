package verifier

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

const meterName = "seidb_flatkv_oracle"

// instrumentation holds the OTel counters and histograms emitted by the
// verifier. All are created lazily in newInstrumentation; writes are safe
// on a nil value (the Go SDK's no-op instrument accepts any call).
type instrumentation struct {
	writeMismatch      metric.Int64Counter
	scanMismatch       metric.Int64Counter
	lthashMismatch     metric.Int64Counter
	historicalMismatch metric.Int64Counter
	scanDuration       metric.Float64Histogram
	scanRows           metric.Int64Counter
	dualWriteLag       metric.Float64Histogram
}

func newInstrumentation() *instrumentation {
	meter := otel.Meter(meterName)
	in := &instrumentation{}
	in.writeMismatch, _ = meter.Int64Counter(
		"flatkv_oracle_write_mismatch_total",
		metric.WithDescription("Oracle 1: per-block write-time diff mismatches"),
	)
	in.scanMismatch, _ = meter.Int64Counter(
		"flatkv_oracle_scan_mismatch_total",
		metric.WithDescription("Oracle 2: forward-subset scan mismatches"),
	)
	in.lthashMismatch, _ = meter.Int64Counter(
		"flatkv_oracle_lthash_mismatch_total",
		metric.WithDescription("Oracle 3: LtHash self-consistency mismatches"),
	)
	in.historicalMismatch, _ = meter.Int64Counter(
		"flatkv_oracle_historical_mismatch_total",
		metric.WithDescription("Oracle 4: historical-version diff mismatches"),
	)
	in.scanDuration, _ = meter.Float64Histogram(
		"flatkv_oracle_scan_duration_seconds",
		metric.WithDescription("Wall time per scan-based oracle run"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)
	in.scanRows, _ = meter.Int64Counter(
		"flatkv_oracle_scan_rows_total",
		metric.WithDescription("Rows examined per scan-based oracle run"),
	)
	in.dualWriteLag, _ = meter.Float64Histogram(
		"flatkv_oracle_dual_write_lag_seconds",
		metric.WithDescription("Time between memiavl commit and flatkv commit in the same block"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)
	return in
}

func (in *instrumentation) recordWriteMismatch(ctx context.Context, reason, store string, n int64) {
	if in == nil || in.writeMismatch == nil || n == 0 {
		return
	}
	in.writeMismatch.Add(ctx, n,
		metric.WithAttributes(
			attribute.String("reason", reason),
			attribute.String("store", store),
		),
	)
}

func (in *instrumentation) recordScanMismatch(ctx context.Context, store string, n int64) {
	if in == nil || in.scanMismatch == nil || n == 0 {
		return
	}
	in.scanMismatch.Add(ctx, n,
		metric.WithAttributes(attribute.String("store", store)),
	)
}

func (in *instrumentation) recordLtHashMismatch(ctx context.Context, n int64) {
	if in == nil || in.lthashMismatch == nil || n == 0 {
		return
	}
	in.lthashMismatch.Add(ctx, n)
}

func (in *instrumentation) recordHistoricalMismatch(ctx context.Context, store string, n int64) {
	if in == nil || in.historicalMismatch == nil || n == 0 {
		return
	}
	in.historicalMismatch.Add(ctx, n,
		metric.WithAttributes(attribute.String("store", store)),
	)
}

func (in *instrumentation) recordScan(ctx context.Context, oracle string, rows int64, seconds float64) {
	if in == nil {
		return
	}
	if in.scanRows != nil && rows > 0 {
		in.scanRows.Add(ctx, rows,
			metric.WithAttributes(attribute.String("oracle", oracle)),
		)
	}
	if in.scanDuration != nil {
		in.scanDuration.Record(ctx, seconds,
			metric.WithAttributes(attribute.String("oracle", oracle)),
		)
	}
}

func (in *instrumentation) recordDualWriteLag(ctx context.Context, seconds float64) {
	if in == nil || in.dualWriteLag == nil {
		return
	}
	in.dualWriteLag.Record(ctx, seconds)
}
