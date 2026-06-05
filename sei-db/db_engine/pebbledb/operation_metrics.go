package pebbledb

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// OperationMetrics records simple, opt-in logical read/write estimates from
// Pebble wrapper hot paths. These are estimates for live read/write ratios, not
// Pebble's internal LSM read amplification.
type OperationMetrics struct {
	databaseName string
	readCounter  metric.Int64Counter
	writeCounter metric.Int64Counter
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

	return &OperationMetrics{
		databaseName: databaseName,
		readCounter:  readCounter,
		writeCounter: writeCounter,
	}
}

func (m *OperationMetrics) AddRead(op string, count int64) {
	if m == nil || count <= 0 {
		return
	}
	m.readCounter.Add(context.Background(), count, metric.WithAttributes(m.attrs(op)...))
}

func (m *OperationMetrics) AddWrite(op string, count int64) {
	if m == nil || count <= 0 {
		return
	}
	m.writeCounter.Add(context.Background(), count, metric.WithAttributes(m.attrs(op)...))
}

func (m *OperationMetrics) attrs(op string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("db", m.databaseName),
		attribute.String("op", op),
	}
}
