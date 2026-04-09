package metrics

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// channelObserver periodically records the depth of registered channels into
// a single OTel Int64Gauge. All methods are nil-receiver safe.
type channelObserver struct {
	mu       sync.Mutex
	channels map[string]func() int
	gauge    metric.Int64Gauge
}

func newChannelObserver(meter metric.Meter) *channelObserver {
	gauge, _ := meter.Int64Gauge(
		"litt_channel_depth",
		metric.WithDescription("Current depth of internal LittDB channels"),
		metric.WithUnit("{count}"),
	)
	return &channelObserver{
		channels: make(map[string]func() int),
		gauge:    gauge,
	}
}

// register adds or replaces a channel size function under the given name.
func (o *channelObserver) register(name string, sizeFunc func() int) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.channels[name] = sizeFunc
}

// collectOnce records the current depth of every registered channel.
func (o *channelObserver) collectOnce() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	ctx := context.Background()
	for name, sizeFunc := range o.channels {
		o.gauge.Record(ctx, int64(sizeFunc()), metric.WithAttributes(attribute.String("channel", name)))
	}
}
