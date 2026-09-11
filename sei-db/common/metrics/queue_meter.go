package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// QueueMeterFactory constructs one shared blocked-time counter and builds a QueueMeter per queue
// recording to it. Use Build() to pair it with that queue's depth gauge.
type QueueMeterFactory struct {
	blockedSeconds metric.Float64Counter
}

// NewQueueMeterFactory creates a factory recording to {prefix}_queue_blocked_seconds_total.
func NewQueueMeterFactory(meter metric.Meter, prefix string) *QueueMeterFactory {
	blockedSeconds, _ := meter.Float64Counter(
		prefix+"_queue_blocked_seconds_total",
		metric.WithDescription("Seconds producers spent blocked waiting for room on a full queue"),
		metric.WithUnit("s"),
	)
	return &QueueMeterFactory{blockedSeconds: blockedSeconds}
}

// Build returns a meter reporting one queue through depth. Every measurement carries attrs, which name
// that queue, and they are resolved once here rather than at each send.
func (f *QueueMeterFactory) Build(depth metric.Int64Gauge, attrs ...attribute.KeyValue) *QueueMeter {
	if f == nil {
		return nil
	}
	return &QueueMeter{
		depth:          depth,
		blockedSeconds: f.blockedSeconds,
		attrs:          metric.WithAttributes(attrs...),
	}
}

// NewQueueMeter creates a factory and builds a single meter from it. Convenient when a prefix has only
// one queue.
func NewQueueMeter(
	meter metric.Meter,
	prefix string,
	depth metric.Int64Gauge,
	attrs ...attribute.KeyValue,
) *QueueMeter {
	return NewQueueMeterFactory(meter, prefix).Build(depth, attrs...)
}

// QueueMeter reports one bounded queue from its producers' side: how full the queue was when a producer
// needed room on it, and how long producers waited when it had none.
//
// Depth alone does not show back-pressure. It is whatever the queue held at the instant it was read, so
// a queue full nearly all the time still reads empty when read just after a consumer drained it, and a
// last-value gauge scraped every few seconds reports one arbitrary moment out of the thousands between
// scrapes. Blocked time accumulates, so a scrape at any cadence reads all of it.
type QueueMeter struct {
	depth          metric.Int64Gauge
	blockedSeconds metric.Float64Counter
	attrs          metric.MeasurementOption
}

// Observe records how full queue is, and must be called before putting anything on it: read after a
// send, the depth is the queue at its emptiest, a consumer having just made the room the send used.
func (m *QueueMeter) Observe(queue int) {
	if m == nil || m.depth == nil {
		return
	}
	m.depth.Record(context.Background(), int64(queue), m.attrs)
}

// Blocked runs send and charges the time it took to the queue, returning send's error unchanged.
//
// Call it only once a non-blocking send has already failed: it reads the clock either side of send,
// which a queue with room in it should not pay for.
func (m *QueueMeter) Blocked(send func() error) error {
	if m == nil || m.blockedSeconds == nil {
		return send()
	}
	started := time.Now()
	err := send()
	m.blockedSeconds.Add(context.Background(), time.Since(started).Seconds(), m.attrs)
	return err
}

// Send puts value on queue, recording the depth the producer found and charging the wait when the queue
// was full. A send that finds room reads no clock.
func Send[T any](m *QueueMeter, queue chan T, value T) {
	m.Observe(len(queue))
	select {
	case queue <- value:
		return
	default:
	}
	_ = m.Blocked(func() error {
		queue <- value
		return nil
	})
}
