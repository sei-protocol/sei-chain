package walrus

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

var meter = otel.Meter("walrus")

// The instruments this package records. PodsProbed is the one the project exists to measure: it is the read
// amplification that pays for never compacting.
var metrics = struct {
	PodsProbed        metric.Int64Histogram
	PodsSearched      metric.Int64Histogram
	QueryDuration     metric.Float64Histogram
	Queries           metric.Int64Counter
	BloomProbes       metric.Int64Counter
	SnapshotReads     metric.Int64Counter
	BlocksAppended    metric.Int64Counter
	EntriesAppended   metric.Int64Counter
	BytesAppended     metric.Int64Counter
	PodsBuilt         metric.Int64Counter
	PodBuildDuration  metric.Float64Histogram
	PodBytes          metric.Int64Gauge
	IndexBytes        metric.Int64Gauge
	BloomBytes        metric.Int64Gauge
	RetainedPods      metric.Int64Gauge
	RetainedSnapshots metric.Int64Gauge
	QueryFloor        metric.Int64Gauge
	FloorBlock        metric.Int64Gauge
	QueriesInFlight   metric.Int64Gauge
	FilesCollected    metric.Int64Counter
	BytesReclaimed    metric.Int64Counter
}{
	PodsProbed: must(meter.Int64Histogram(
		"walrus_pods_probed",
		metric.WithDescription("Number of pod bloom filters tested to answer one query"),
		metric.WithUnit("{count}"),
		metric.WithExplicitBucketBoundaries(commonmetrics.CountBuckets...),
	)),
	PodsSearched: must(meter.Int64Histogram(
		"walrus_pods_searched",
		metric.WithDescription("Number of pod indexes searched to answer one query"),
		metric.WithUnit("{count}"),
		metric.WithExplicitBucketBoundaries(commonmetrics.CountBuckets...),
	)),
	QueryDuration: must(meter.Float64Histogram(
		"walrus_query_duration",
		metric.WithDescription("Time to answer one historical read"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)),
	Queries: must(meter.Int64Counter(
		"walrus_queries",
		metric.WithDescription("Number of historical reads, by how they resolved"),
		metric.WithUnit("{count}"),
	)),
	BloomProbes: must(meter.Int64Counter(
		"walrus_bloom_probes",
		metric.WithDescription("Number of pod bloom filter tests, by whether the pod really held the key"),
		metric.WithUnit("{count}"),
	)),
	SnapshotReads: must(meter.Int64Counter(
		"walrus_snapshot_reads",
		metric.WithDescription("Number of walks that ran out of pods and read from a snapshot"),
		metric.WithUnit("{count}"),
	)),
	BlocksAppended: must(meter.Int64Counter(
		"walrus_blocks_appended",
		metric.WithDescription("Number of blocks appended to pods"),
		metric.WithUnit("{count}"),
	)),
	EntriesAppended: must(meter.Int64Counter(
		"walrus_entries_appended",
		metric.WithDescription("Number of key changes appended to pods"),
		metric.WithUnit("{count}"),
	)),
	BytesAppended: must(meter.Int64Counter(
		"walrus_bytes_appended",
		metric.WithDescription("Number of bytes appended to pod data files"),
		metric.WithUnit("By"),
	)),
	PodsBuilt: must(meter.Int64Counter(
		"walrus_pods_built",
		metric.WithDescription("Number of pods written"),
		metric.WithUnit("{count}"),
	)),
	PodBuildDuration: must(meter.Float64Histogram(
		"walrus_pod_build_duration",
		metric.WithDescription("Time to write one pod's data file, index, and bloom filter"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
	)),
	PodBytes: must(meter.Int64Gauge(
		"walrus_pod_bytes",
		metric.WithDescription("Bytes held by retained pod data files"),
		metric.WithUnit("By"),
	)),
	IndexBytes: must(meter.Int64Gauge(
		"walrus_index_bytes",
		metric.WithDescription("Bytes held by retained pod indexes"),
		metric.WithUnit("By"),
	)),
	BloomBytes: must(meter.Int64Gauge(
		"walrus_bloom_bytes",
		metric.WithDescription("Bytes held by retained pod bloom filters"),
		metric.WithUnit("By"),
	)),
	RetainedPods: must(meter.Int64Gauge(
		"walrus_retained_pods",
		metric.WithDescription("Number of pods inside the retention window"),
		metric.WithUnit("{count}"),
	)),
	RetainedSnapshots: must(meter.Int64Gauge(
		"walrus_retained_snapshots",
		metric.WithDescription("Number of retained snapshots inside the retention window"),
		metric.WithUnit("{count}"),
	)),
	QueryFloor: must(meter.Int64Gauge(
		"walrus_query_floor",
		metric.WithDescription("Oldest block policy permits a query to reach"),
	)),
	FloorBlock: must(meter.Int64Gauge(
		"walrus_floor_block",
		metric.WithDescription("Block of the snapshot backwards walks terminate at"),
	)),
	QueriesInFlight: must(meter.Int64Gauge(
		"walrus_queries_in_flight",
		metric.WithDescription("Number of admitted queries holding references"),
		metric.WithUnit("{count}"),
	)),
	FilesCollected: must(meter.Int64Counter(
		"walrus_files_collected",
		metric.WithDescription("Number of pod and snapshot files deleted by collection"),
		metric.WithUnit("{count}"),
	)),
	BytesReclaimed: must(meter.Int64Counter(
		"walrus_bytes_reclaimed",
		metric.WithDescription("Bytes freed by collection"),
		metric.WithUnit("By"),
	)),
}

// must panics if instrument construction failed. An instrument that cannot be created is a programming error
// in the declaration above, not a runtime condition, so there is nothing for a caller to handle.
func must[V any](instrument V, err error) V {
	if err != nil {
		panic(err)
	}
	return instrument
}

// recordQuery records one answered query against the instance named by name.
//
// podsProbed counts the pods whose bloom filter was tested and podsSearched the subset whose index was then
// searched, so the gap between podsSearched and the pods that really held the key is the false positive cost.
// readSnapshot reports whether the walk ran out of pods and read the floor.
func recordQuery(
	name string,
	start time.Time,
	status ReadStatus,
	podsProbed int,
	podsSearched int,
	readSnapshot bool,
) {
	attrs := metric.WithAttributes(
		attribute.String("walrus", name),
		attribute.String("status", status.String()),
	)
	ctx := context.Background()
	metrics.Queries.Add(ctx, 1, attrs)
	metrics.QueryDuration.Record(ctx, time.Since(start).Seconds(), attrs)
	metrics.PodsProbed.Record(ctx, int64(podsProbed), attrs)
	metrics.PodsSearched.Record(ctx, int64(podsSearched), attrs)
	if readSnapshot {
		metrics.SnapshotReads.Add(ctx, 1, attrs)
	}
}

// recordBloomProbe records one bloom filter test. hit reports whether the pod really held the key, so that a
// probe counted as a hit here but not confirmed by the index is a false positive.
func recordBloomProbe(name string, ruledOut bool, hit bool) {
	outcome := "hit"
	switch {
	case ruledOut:
		outcome = "ruled_out"
	case !hit:
		outcome = "false_positive"
	}
	metrics.BloomProbes.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("walrus", name),
		attribute.String("outcome", outcome),
	))
}

// recordAppend records one appended block.
func recordAppend(name string, entries int, bytes int) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	metrics.BlocksAppended.Add(ctx, 1, attrs)
	metrics.EntriesAppended.Add(ctx, int64(entries), attrs)
	metrics.BytesAppended.Add(ctx, int64(bytes), attrs)
}

// recordPodBuild records one written pod and the time it took.
func recordPodBuild(name string, start time.Time) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	metrics.PodsBuilt.Add(context.Background(), 1, attrs)
	metrics.PodBuildDuration.Record(
		context.Background(),
		time.Since(start).Seconds(),
		attrs,
	)
}

// recordCollection records what one collection pass deleted.
func recordCollection(name string, files int, bytes int64) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	metrics.FilesCollected.Add(ctx, int64(files), attrs)
	metrics.BytesReclaimed.Add(ctx, bytes, attrs)
}

// recordFloors records where policy and the floor snapshot currently sit, and how many queries are running.
func recordFloors(name string, queryFloor uint64, floorBlock uint64, inFlight int) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	//nolint:gosec // G115 - block numbers are far below the int64 ceiling
	metrics.QueryFloor.Record(ctx, int64(queryFloor), attrs)
	//nolint:gosec // G115 - block numbers are far below the int64 ceiling
	metrics.FloorBlock.Record(ctx, int64(floorBlock), attrs)
	metrics.QueriesInFlight.Record(ctx, int64(inFlight), attrs)
}

// recordRetention records the space and count the retained pods and snapshots occupy.
func recordRetention(name string, podBytes int64, indexBytes int64, bloomBytes int64, pods int64, snapshots int64) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	metrics.PodBytes.Record(ctx, podBytes, attrs)
	metrics.IndexBytes.Record(ctx, indexBytes, attrs)
	metrics.BloomBytes.Record(ctx, bloomBytes, attrs)
	metrics.RetainedPods.Record(ctx, pods, attrs)
	metrics.RetainedSnapshots.Record(ctx, snapshots, attrs)
}
