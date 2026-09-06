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

// Instrument names carry their own unit and counter suffixes, so the name in the code is the name Prometheus
// scrapes. The exporter would otherwise append them, and a name that cannot be grepped from a dashboard back
// to the line that records it is a name that goes stale without anyone noticing.

// The phase of a read that a duration was spent in.
const phaseBloom = "bloom"

// The phase of a read spent searching pod indexes.
const phaseIndex = "index"

// The phase of a read spent reading the entry out of a pod's data file.
const phaseData = "data"

// The phase of a read spent reading the floor snapshot.
const phaseSnapshot = "snapshot"

// The bloom outcome when a filter correctly ruled a pod out.
const bloomRuledOut = "ruled_out"

// The bloom outcome when a filter admitted a pod that really did hold the key.
const bloomTruePositive = "true_positive"

// The bloom outcome when a filter admitted a pod that did not hold the key, costing an index search for
// nothing. The observed false positive rate is this over the pods that did not hold the key.
const bloomFalsePositive = "false_positive"

// The pod file a byte count belongs to.
const fileData = "data"

// The searched half of the pod index.
const fileHashIndex = "hash"

// The dereferenced half of the pod index.
const fileVersionIndex = "version"

// The pod bloom filter.
const fileBloom = "bloom"

// The instruments this package records. walrus_pods_probed is the one the project exists to measure: it is
// the read amplification that buys writing every entry exactly once.
var metrics = struct {
	PodsProbed        metric.Int64Histogram
	PodsSearched      metric.Int64Histogram
	QueryDuration     metric.Float64Histogram
	ReadPhaseDuration metric.Float64Histogram
	Queries           metric.Int64Counter
	BloomProbes       metric.Int64Counter
	SnapshotReads     metric.Int64Counter
	BlocksAppended    metric.Int64Counter
	EntriesAppended   metric.Int64Counter
	AppendedBytes     metric.Int64Counter
	AppendBlockedTime metric.Float64Counter
	WrittenBytes      metric.Int64Counter
	PodsBuilt         metric.Int64Counter
	PodBuildDuration  metric.Float64Histogram
	PodKeys           metric.Int64Histogram
	PodEntries        metric.Int64Histogram
	PodBlocks         metric.Int64Histogram
	BuildQueueDepth   metric.Int64Gauge
	PodBytes          metric.Int64Gauge
	IndexBytes        metric.Int64Gauge
	BloomBytes        metric.Int64Gauge
	SnapshotBytes     metric.Int64Gauge
	RetainedPods      metric.Int64Gauge
	RetainedSnapshots metric.Int64Gauge
	QueryFloor        metric.Int64Gauge
	FloorBlock        metric.Int64Gauge
	QueriesInFlight   metric.Int64Gauge
	FilesCollected    metric.Int64Counter
	ReclaimedBytes    metric.Int64Counter
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
		"walrus_query_duration_seconds",
		metric.WithDescription("Time to answer one historical read"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)),
	ReadPhaseDuration: must(meter.Float64Histogram(
		"walrus_read_phase_duration_seconds",
		metric.WithDescription("Time one read spent in each phase of the walk"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)),
	Queries: must(meter.Int64Counter(
		"walrus_queries_total",
		metric.WithDescription("Number of historical reads, by how they resolved"),
		metric.WithUnit("{count}"),
	)),
	BloomProbes: must(meter.Int64Counter(
		"walrus_bloom_probes_total",
		metric.WithDescription("Number of pod bloom filter tests, by whether the pod really held the key"),
		metric.WithUnit("{count}"),
	)),
	SnapshotReads: must(meter.Int64Counter(
		"walrus_snapshot_reads_total",
		metric.WithDescription("Number of walks that ran out of pods and read from a snapshot"),
		metric.WithUnit("{count}"),
	)),
	BlocksAppended: must(meter.Int64Counter(
		"walrus_blocks_appended_total",
		metric.WithDescription("Number of blocks appended"),
		metric.WithUnit("{count}"),
	)),
	EntriesAppended: must(meter.Int64Counter(
		"walrus_entries_appended_total",
		metric.WithDescription("Number of key changes appended"),
		metric.WithUnit("{count}"),
	)),
	AppendedBytes: must(meter.Int64Counter(
		"walrus_appended_bytes_total",
		metric.WithDescription("Bytes of key changes appended, before any index or filter is built"),
		metric.WithUnit("By"),
	)),
	AppendBlockedTime: must(meter.Float64Counter(
		"walrus_append_blocked_seconds_total",
		metric.WithDescription("Time appends spent waiting for a pod build slot"),
		metric.WithUnit("s"),
	)),
	WrittenBytes: must(meter.Int64Counter(
		"walrus_written_bytes_total",
		metric.WithDescription("Bytes written to disk, by which of a pod's three files they went to"),
		metric.WithUnit("By"),
	)),
	PodsBuilt: must(meter.Int64Counter(
		"walrus_pods_built_total",
		metric.WithDescription("Number of pods written"),
		metric.WithUnit("{count}"),
	)),
	PodBuildDuration: must(meter.Float64Histogram(
		"walrus_pod_build_duration_seconds",
		metric.WithDescription("Time to write one pod's data file, index, and bloom filter"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LongLatencyBuckets...),
	)),
	PodKeys: must(meter.Int64Histogram(
		"walrus_pod_keys",
		metric.WithDescription("Distinct keys in one pod"),
		metric.WithUnit("{count}"),
		metric.WithExplicitBucketBoundaries(commonmetrics.CountBuckets...),
	)),
	PodEntries: must(meter.Int64Histogram(
		"walrus_pod_entries",
		metric.WithDescription("Entries in one pod, counting every version of every key"),
		metric.WithUnit("{count}"),
		metric.WithExplicitBucketBoundaries(commonmetrics.CountBuckets...),
	)),
	PodBlocks: must(meter.Int64Histogram(
		"walrus_pod_blocks",
		metric.WithDescription("Blocks in one pod, which converts pods probed into blocks probed"),
		metric.WithUnit("{count}"),
		metric.WithExplicitBucketBoundaries(commonmetrics.CountBuckets...),
	)),
	BuildQueueDepth: must(meter.Int64Gauge(
		"walrus_build_queue_depth",
		metric.WithDescription("Pods being built or waiting to be built"),
		metric.WithUnit("{count}"),
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
	SnapshotBytes: must(meter.Int64Gauge(
		"walrus_snapshot_bytes",
		metric.WithDescription("Bytes held by retained snapshots"),
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
		"walrus_files_collected_total",
		metric.WithDescription("Number of pod and snapshot files deleted by collection"),
		metric.WithUnit("{count}"),
	)),
	ReclaimedBytes: must(meter.Int64Counter(
		"walrus_reclaimed_bytes_total",
		metric.WithDescription("Bytes freed by collection"),
		metric.WithUnit("By"),
	)),
}

// must panics if instrument construction failed, which is a fault in the declaration above rather than a
// runtime condition a caller could handle.
func must[V any](instrument V, err error) V {
	if err != nil {
		panic(err)
	}
	return instrument
}

// walkTiming is how long one read spent in each phase of the walk.
//
// The phases are accumulated across the pods a walk visits and recorded once, rather than per pod, so a walk
// over a hundred pods costs one observation per phase instead of a hundred.
type walkTiming struct {
	bloom    time.Duration
	index    time.Duration
	data     time.Duration
	snapshot time.Duration
}

// recordQuery records one answered query.
//
// podsProbed counts the pods whose bloom filter was tested and podsSearched the subset whose index was then
// searched. readSnapshot reports whether the walk ran out of pods and read the floor.
func recordQuery(
	name string,
	start time.Time,
	status ReadStatus,
	podsProbed int,
	podsSearched int,
	readSnapshot bool,
	timing walkTiming,
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

	recordReadPhase(ctx, name, phaseBloom, timing.bloom)
	recordReadPhase(ctx, name, phaseIndex, timing.index)
	recordReadPhase(ctx, name, phaseData, timing.data)
	recordReadPhase(ctx, name, phaseSnapshot, timing.snapshot)
}

// recordReadPhase records the time one read spent in one phase.
func recordReadPhase(ctx context.Context, name string, phase string, elapsed time.Duration) {
	metrics.ReadPhaseDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		attribute.String("walrus", name),
		attribute.String("phase", phase),
	))
}

// recordBloomProbe records one bloom filter test.
//
// outcome is one of the bloom constants above. The observed false positive rate is the false positives over
// the pods that did not hold the key, which is false_positive / (false_positive + ruled_out).
func recordBloomProbe(name string, outcome string) {
	metrics.BloomProbes.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("walrus", name),
		attribute.String("outcome", outcome),
	))
}

// recordAppend records one appended block.
func recordAppend(name string, entries int, bytes int, blocked time.Duration) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	metrics.BlocksAppended.Add(ctx, 1, attrs)
	metrics.EntriesAppended.Add(ctx, int64(entries), attrs)
	metrics.AppendedBytes.Add(ctx, int64(bytes), attrs)
	if blocked > 0 {
		metrics.AppendBlockedTime.Add(ctx, blocked.Seconds(), attrs)
	}
}

// recordPodBuild records one written pod: what it took, what it holds, and what it cost on disk.
func recordPodBuild(name string, start time.Time, shape podShape) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()

	metrics.PodsBuilt.Add(ctx, 1, attrs)
	metrics.PodBuildDuration.Record(ctx, time.Since(start).Seconds(), attrs)
	metrics.PodKeys.Record(ctx, shape.keys, attrs)
	metrics.PodEntries.Record(ctx, shape.entries, attrs)
	metrics.PodBlocks.Record(ctx, shape.blocks, attrs)

	recordWrittenBytes(ctx, name, fileData, shape.dataBytes)
	recordWrittenBytes(ctx, name, fileHashIndex, shape.hashBytes)
	recordWrittenBytes(ctx, name, fileVersionIndex, shape.versionBytes)
	recordWrittenBytes(ctx, name, fileBloom, shape.bloomBytes)
}

// recordWrittenBytes records the bytes one of a pod's files cost to write.
func recordWrittenBytes(ctx context.Context, name string, file string, bytes int64) {
	metrics.WrittenBytes.Add(ctx, bytes, metric.WithAttributes(
		attribute.String("walrus", name),
		attribute.String("file", file),
	))
}

// recordBuildQueueDepth records how many pods are building or waiting to build.
func recordBuildQueueDepth(name string, depth int) {
	metrics.BuildQueueDepth.Record(context.Background(), int64(depth),
		metric.WithAttributes(attribute.String("walrus", name)))
}

// recordCollection records what one collection pass deleted.
func recordCollection(name string, files int, bytes int64) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	metrics.FilesCollected.Add(ctx, int64(files), attrs)
	metrics.ReclaimedBytes.Add(ctx, bytes, attrs)
}

// recordFloors records where policy and the floor snapshot currently sit, and how many queries are running.
func recordFloors(name string, queryFloor uint64, floorBlock uint64, inFlight int) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	//nolint:gosec // G115 - block numbers stay far below the int64 ceiling
	metrics.QueryFloor.Record(ctx, int64(queryFloor), attrs)
	//nolint:gosec // G115 - as above
	metrics.FloorBlock.Record(ctx, int64(floorBlock), attrs)
	metrics.QueriesInFlight.Record(ctx, int64(inFlight), attrs)
}

// recordRetention records the space and count the retained pods and snapshots occupy.
func recordRetention(
	name string,
	podBytes int64,
	indexBytes int64,
	bloomBytes int64,
	snapshotBytes int64,
	pods int64,
	snapshots int64,
) {
	attrs := metric.WithAttributes(attribute.String("walrus", name))
	ctx := context.Background()
	metrics.PodBytes.Record(ctx, podBytes, attrs)
	metrics.IndexBytes.Record(ctx, indexBytes, attrs)
	metrics.BloomBytes.Record(ctx, bloomBytes, attrs)
	metrics.SnapshotBytes.Record(ctx, snapshotBytes, attrs)
	metrics.RetainedPods.Record(ctx, pods, attrs)
	metrics.RetainedSnapshots.Record(ctx, snapshots, attrs)
}
