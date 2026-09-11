package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// QueueMeter records how long producers spend blocked on one bounded queue, which is what
// back-pressure from that queue costs the stage feeding it.
//
// Only the blocking path is measured, so a send that finds room reads no clock and a queue nobody
// waits on reports nothing at all. How full a queue sits in the ordinary case is left to each store's
// periodic collector: that depth is a last-value gauge, so sampling it at every send would write
// thousands of values between scrapes that keep one.
//
// Send covers a plain channel and is the usual way to use it. A caller whose send cannot be written
// that way — one selecting on a cancellation channel alongside the send — hands the two halves of its
// own send to SendVia. SampleDepth adds the depth, for a queue whose owner has no collector of its
// own to report it from.
//
// A nil meter is usable and records nothing, which is what metrics being disabled looks like.
type QueueMeter struct {
	meter          metric.Meter
	prefix         string
	blockedSeconds metric.Float64Counter
	attrs          metric.MeasurementOption
}

// NewQueueMeter returns a meter recording to {prefix}_queue_blocked_seconds_total. Every measurement
// carries attrs, which name this queue among the others sharing the prefix.
func NewQueueMeter(meter metric.Meter, prefix string, attrs ...attribute.KeyValue) *QueueMeter {
	blockedSeconds, _ := meter.Float64Counter(
		prefix+"_queue_blocked_seconds_total",
		metric.WithDescription("Seconds producers spent blocked waiting for room on a full queue"),
		metric.WithUnit("s"),
	)
	return &QueueMeter{
		meter:          meter,
		prefix:         prefix,
		blockedSeconds: blockedSeconds,
		attrs:          metric.WithAttributes(attrs...),
	}
}

// SampleDepth reports how full the queue is to {prefix}_queue_depth, every intervalSeconds until ctx
// is cancelled. depth is called from another goroutine, which len on a channel tolerates.
//
// Sampling on a timer rather than at each send is what the gauge is worth: a scrape keeps the last
// value written, so recording at every send would write thousands between scrapes that keep one. A
// queue whose owner already has a periodic collector should report the depth from there instead.
func (m *QueueMeter) SampleDepth(ctx context.Context, intervalSeconds int, depth func() int) {
	if m == nil || m.meter == nil || depth == nil {
		return
	}
	gauge, _ := m.meter.Int64Gauge(
		m.prefix+"_queue_depth",
		metric.WithDescription("Messages waiting on the queue, sampled on the collection interval"),
		metric.WithUnit("{message}"),
	)
	StartPeriodicSampling(ctx, intervalSeconds, func() {
		gauge.Record(context.Background(), int64(depth()), m.attrs)
	})
}

// SendVia performs a send the caller describes in two halves: try attempts a non-blocking send and
// reports whether it succeeded, and send completes a blocking one, returning its error unchanged.
// Only the second is timed. Use Send for a plain channel and this for anything Send cannot express.
func (m *QueueMeter) SendVia(try func() bool, send func() error) error {
	if try() {
		return nil
	}
	return m.blocked(send)
}

// Send puts value on queue, charging it the wait when the queue was full.
func Send[T any](m *QueueMeter, queue chan<- T, value T) {
	_ = m.SendVia(
		func() bool {
			select {
			case queue <- value:
				return true
			default:
				return false
			}
		},
		func() error {
			queue <- value
			return nil
		},
	)
}

// blocked runs send and charges the time it took to the queue. It reads the clock either side of
// send, which is why only the path taken once the queue is known to be full reaches it.
func (m *QueueMeter) blocked(send func() error) error {
	if m == nil || m.blockedSeconds == nil {
		return send()
	}
	started := time.Now()
	err := send()
	m.blockedSeconds.Add(context.Background(), time.Since(started).Seconds(), m.attrs)
	return err
}
