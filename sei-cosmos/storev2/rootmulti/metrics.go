package rootmulti

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seicosmos_storev2_rootmulti")

	// finerGrainedBuckets units are in seconds
	finerGrainedBuckets = metric.WithExplicitBucketBoundaries(
		0.000025, 0.000050, 0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.010, 0.020, 0.050, 0.075, 0.1, 0.25, 0.5, 1, 10,
	)

	storev2Metrics = struct {
		scCommitLatency       metric.Float64Histogram
		ssVersion             metric.Int64Gauge
		historicalAbciQuery   metric.Int64Counter
		iavlTotalKeyBytes     metric.Int64Gauge
		iavlTotalValueBytes   metric.Int64Gauge
		iavlTotalNumKeys      metric.Int64Gauge
		stateSyncKeysExported metric.Int64Counter
	}{
		scCommitLatency: must(meter.Float64Histogram(
			"sc_commit_latency",
			metric.WithDescription("Duration of SC store commit in seconds"),
			finerGrainedBuckets,
			metric.WithUnit("s"),
		)),
		ssVersion: must(meter.Int64Gauge(
			"ss_version",
			metric.WithDescription("Current SS store version"),
			metric.WithUnit("{version}"),
		)),
		historicalAbciQuery: must(meter.Int64Counter(
			"historical_abci_query",
			metric.WithDescription("Number of historical ABCI queries by success and proof status"),
			metric.WithUnit("{count}"),
		)),
		iavlTotalKeyBytes: must(meter.Int64Gauge(
			"iavl_total_key_bytes",
			metric.WithDescription("Total key bytes per store in IAVL snapshot"),
			metric.WithUnit("By"),
		)),
		iavlTotalValueBytes: must(meter.Int64Gauge(
			"iavl_total_value_bytes",
			metric.WithDescription("Total value bytes per store in IAVL snapshot"),
			metric.WithUnit("By"),
		)),
		iavlTotalNumKeys: must(meter.Int64Gauge(
			"iavl_total_num_keys",
			metric.WithDescription("Total number of keys per store in IAVL snapshot"),
			metric.WithUnit("{count}"),
		)),
		stateSyncKeysExported: must(meter.Int64Counter(
			"state_sync_keys_exported",
			metric.WithDescription("Number of keys exported during state sync"),
			metric.WithUnit("{count}"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
