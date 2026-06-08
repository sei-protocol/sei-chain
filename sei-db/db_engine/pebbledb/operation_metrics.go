package pebbledb

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// OperationMetrics records simple, opt-in logical read/write estimates from
// Pebble-backed hot paths. Read amplification can be approximated from these
// counters as estimated reads divided by estimated writes.
type OperationMetrics struct {
	readCounter  metric.Int64Counter
	writeCounter metric.Int64Counter
	addOpt       metric.AddOption
}

func NewOperationMetrics(enabled bool, databaseName string) *OperationMetrics {
	if !enabled {
		return nil
	}

	meter := otel.Meter(pebbleMeterName)
	readCounter, _ := meter.Int64Counter(
		"pebble_estimated_reads",
		metric.WithDescription("Estimated logical PebbleDB reads observed by SeiDB wrappers"),
		metric.WithUnit("{count}"),
	)
	writeCounter, _ := meter.Int64Counter(
		"pebble_estimated_writes",
		metric.WithDescription("Estimated logical PebbleDB writes observed by SeiDB wrappers"),
		metric.WithUnit("{count}"),
	)
	attrs := attribute.NewSet(attribute.String("db", databaseName))

	return &OperationMetrics{
		readCounter:  readCounter,
		writeCounter: writeCounter,
		addOpt:       metric.WithAttributeSet(attrs),
	}
}

func (m *OperationMetrics) AddRead(count int64) {
	if m == nil || count <= 0 {
		return
	}
	m.readCounter.Add(context.Background(), count, m.addOpt)
}

func (m *OperationMetrics) AddWrite(count int64) {
	if m == nil || count <= 0 {
		return
	}
	m.writeCounter.Add(context.Background(), count, m.addOpt)
}
