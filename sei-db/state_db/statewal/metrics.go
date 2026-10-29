package statewal

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

var (
	stateWALMeter = otel.Meter("seidb_state_wal")

	// writePhases times each stage of a block's write on the WAL goroutine.
	writePhases = metrics.NewPhaseTimerFactory(stateWALMeter, "seidb_state_wal_write").RecordLatencies()

	// queueDepth is the number of requests waiting behind the one the WAL goroutine just took.
	queueDepth, _ = stateWALMeter.Int64Gauge(
		"seidb_state_wal_queue_depth",
		metric.WithDescription("Requests queued for the state WAL goroutine when it takes a write"),
		metric.WithUnit("{request}"),
	)
)
