package lakesim

import (
	"context"
	"math"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// lakesimMeterName is the OTel meter every lakesim instrument is created on.
const lakesimMeterName = "lakesim"

// The producer's phases, as the phase label on lakesim_producer_phase_duration_seconds_total.
const (
	// Waiting for the block rate limit.
	phaseThrottle = "throttle"

	// Waiting for a lane to accept the block.
	phaseBackpressure = "backpressure"
)

// LakesimMetrics holds the OpenTelemetry instruments for the lakesim benchmark.
type LakesimMetrics struct {
	blocksTotal       metric.Int64Counter
	putsTotal         metric.Int64Counter
	bytesWrittenTotal metric.Int64Counter

	putCallSeconds    metric.Float64Histogram
	putDurableSeconds metric.Float64Histogram

	inFlightWrites    metric.Int64Gauge
	durableWatermark  metric.Int64Gauge
	deletionWatermark metric.Int64Gauge
	retainedBlocks    metric.Int64Gauge
	retainedBytes     metric.Int64Gauge

	producerPhases *metrics.PhaseTimer
}

// NewLakesimMetrics creates the benchmark's instruments on the global OTel MeterProvider, which the caller must
// configure first.
func NewLakesimMetrics() *LakesimMetrics {
	meter := otel.Meter(lakesimMeterName)

	blocksTotal, _ := meter.Int64Counter(
		"lakesim_blocks_total",
		metric.WithDescription("Blocks handed to every lane"),
		metric.WithUnit("{count}"),
	)
	putsTotal, _ := meter.Int64Counter(
		"lakesim_puts_total",
		metric.WithDescription("Puts accepted by a lane's data lake"),
		metric.WithUnit("{count}"),
	)
	bytesWrittenTotal, _ := meter.Int64Counter(
		"lakesim_bytes_written_total",
		metric.WithDescription("Value bytes in puts accepted by a lane's data lake"),
		metric.WithUnit("By"),
	)
	putCallSeconds, _ := meter.Float64Histogram(
		"lakesim_put_call_seconds",
		metric.WithDescription("Time a Put() call blocks, including the data lake's backpressure"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)
	putDurableSeconds, _ := meter.Float64Histogram(
		"lakesim_put_durable_seconds",
		metric.WithDescription("Time from a put's acceptance until the durable watermark passes it"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)
	inFlightWrites, _ := meter.Int64Gauge(
		"lakesim_in_flight_writes",
		metric.WithDescription("Accepted puts at or above the durable watermark"),
		metric.WithUnit("{count}"),
	)
	durableWatermark, _ := meter.Int64Gauge(
		"lakesim_durable_watermark",
		metric.WithDescription("The data lake's durable watermark"),
		metric.WithUnit("{key}"),
	)
	deletionWatermark, _ := meter.Int64Gauge(
		"lakesim_deletion_watermark",
		metric.WithDescription("The data lake's deletion watermark"),
		metric.WithUnit("{key}"),
	)
	retainedBlocks, _ := meter.Int64Gauge(
		"lakesim_retained_blocks",
		metric.WithDescription("Blocks whose objects may still exist in the bucket"),
		metric.WithUnit("{count}"),
	)
	retainedBytes, _ := meter.Int64Gauge(
		"lakesim_retained_bytes",
		metric.WithDescription("Value bytes of the blocks whose objects may still exist in the bucket"),
		metric.WithUnit("By"),
	)

	return &LakesimMetrics{
		blocksTotal:       blocksTotal,
		putsTotal:         putsTotal,
		bytesWrittenTotal: bytesWrittenTotal,
		putCallSeconds:    putCallSeconds,
		putDurableSeconds: putDurableSeconds,
		inFlightWrites:    inFlightWrites,
		durableWatermark:  durableWatermark,
		deletionWatermark: deletionWatermark,
		retainedBlocks:    retainedBlocks,
		retainedBytes:     retainedBytes,
		producerPhases:    metrics.NewPhaseTimer(meter, "lakesim_producer"),
	}
}

// LaneAttributes returns the measurement option that labels a lane's measurements.
func LaneAttributes(laneName string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("lane", laneName))
}

// ReportBlock records a block handed to every lane.
func (m *LakesimMetrics) ReportBlock() {
	m.blocksTotal.Add(context.Background(), 1)
}

// SetProducerPhase records the producer entering phase.
func (m *LakesimMetrics) SetProducerPhase(phase string) {
	m.producerPhases.SetPhase(phase)
}

// EndProducerPhase records the end of the producer's current phase.
func (m *LakesimMetrics) EndProducerPhase() {
	m.producerPhases.Reset()
}

// ReportPut records a put a lane's data lake accepted.
func (m *LakesimMetrics) ReportPut(
	// The lane's attributes, from LaneAttributes().
	lane metric.MeasurementOption,
	// The size of the value.
	valueSize int,
	// How long the Put() call blocked.
	callTime time.Duration,
) {
	ctx := context.Background()
	m.putsTotal.Add(ctx, 1, lane)
	m.bytesWrittenTotal.Add(ctx, int64(valueSize), lane)
	m.putCallSeconds.Record(ctx, callTime.Seconds(), lane)
}

// ReportDurable records the time from a put's acceptance until it became durable.
func (m *LakesimMetrics) ReportDurable(
	// The lane's attributes, from LaneAttributes().
	lane metric.MeasurementOption,
	latency time.Duration,
) {
	m.putDurableSeconds.Record(context.Background(), latency.Seconds(), lane)
}

// ReportWatermarks records a lane's watermarks and what they imply about its writes and stored data.
func (m *LakesimMetrics) ReportWatermarks(
	// The lane's attributes, from LaneAttributes().
	lane metric.MeasurementOption,
	// One above the highest key the lane has put.
	nextKey uint64,
	// The data lake's durable watermark.
	durableWatermark uint64,
	// The data lake's deletion watermark.
	deletionWatermark uint64,
	// The size of each of the lane's values.
	valueSize int,
) {
	ctx := context.Background()
	retained := nextKey - min(deletionWatermark, nextKey)
	m.inFlightWrites.Record(ctx, clampToInt64(nextKey-min(durableWatermark, nextKey)), lane)
	m.durableWatermark.Record(ctx, clampToInt64(durableWatermark), lane)
	m.deletionWatermark.Record(ctx, clampToInt64(deletionWatermark), lane)
	m.retainedBlocks.Record(ctx, clampToInt64(retained), lane)
	m.retainedBytes.Record(ctx, clampToInt64(retained)*int64(valueSize), lane)
}

// clampToInt64 converts value to an int64, clamping it to math.MaxInt64.
func clampToInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
