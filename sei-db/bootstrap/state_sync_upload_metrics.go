package bootstrap

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

const stateSyncUploadMeterName = "seidb_state_sync_upload"

// stateSyncUploadMetrics are the metrics of the state sync archive upload of one node.
type stateSyncUploadMetrics struct {
	localCheckpointHeight metric.Int64Gauge
	bucketHeight          metric.Int64Gauge
	bucketTime            metric.Int64Gauge
	archiveSize           metric.Int64Gauge
	uploadDuration        metric.Float64Histogram
	failures              metric.Int64Counter
}

// newStateSyncUploadMetrics returns the state sync upload metrics of meter.
func newStateSyncUploadMetrics(meter metric.Meter) *stateSyncUploadMetrics {
	return &stateSyncUploadMetrics{
		localCheckpointHeight: must(meter.Int64Gauge(
			"state_sync_upload_local_checkpoint_height",
			metric.WithDescription("Height of the newest FlatKV checkpoint of this node that has a state sync proof file"),
			metric.WithUnit("{height}"),
		)),
		bucketHeight: must(meter.Int64Gauge(
			"state_sync_upload_bucket_height",
			metric.WithDescription("Height of the newest state sync archive of the chain in the bucket, as this node last read or wrote it"),
			metric.WithUnit("{height}"),
		)),
		bucketTime: must(meter.Int64Gauge(
			"state_sync_upload_bucket_time",
			metric.WithDescription("Unix time at which the bucket recorded the newest state sync archive of the chain, as this node last read or wrote it"),
			metric.WithUnit("s"),
		)),
		archiveSize: must(meter.Int64Gauge(
			"state_sync_upload_archive_size",
			metric.WithDescription("Size of the last state sync archive that this node uploaded"),
			metric.WithUnit("By"),
		)),
		uploadDuration: must(meter.Float64Histogram(
			"state_sync_upload_duration",
			metric.WithDescription("Time that this node took to stage and upload one state sync archive"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
		)),
		failures: must(meter.Int64Counter(
			"state_sync_upload_failures",
			metric.WithDescription("Number of state sync upload sweeps that failed and will be retried"),
			metric.WithUnit("{count}"),
		)),
	}
}

// defaultStateSyncUploadMetrics returns the state sync upload metrics of the global meter provider.
func defaultStateSyncUploadMetrics() *stateSyncUploadMetrics {
	return newStateSyncUploadMetrics(otel.Meter(stateSyncUploadMeterName))
}

func (m *stateSyncUploadMetrics) recordLocalCheckpoint(ctx context.Context, height int64) {
	m.localCheckpointHeight.Record(ctx, height)
}

func (m *stateSyncUploadMetrics) recordBucket(ctx context.Context, height int64, modified time.Time) {
	m.bucketHeight.Record(ctx, height)
	m.bucketTime.Record(ctx, modified.Unix())
}

func (m *stateSyncUploadMetrics) recordUpload(ctx context.Context, size int64, duration time.Duration) {
	m.archiveSize.Record(ctx, size)
	m.uploadDuration.Record(ctx, duration.Seconds())
}

func (m *stateSyncUploadMetrics) recordFailure(ctx context.Context) {
	m.failures.Add(ctx, 1)
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
