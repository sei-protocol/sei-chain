package app

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestInitAppMetricsNoPanic(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	initAppMetrics()
}

func TestAppMetricsAllInstrumentsUsable(t *testing.T) {
	otel.SetMeterProvider(noop.NewMeterProvider())
	initAppMetrics()

	ctx := t.Context()

	appMetrics.beginBlockDuration.Record(ctx, 0.1)
	appMetrics.endBlockDuration.Record(ctx, 0.2)
	appMetrics.moduleEndBlockDuration.Record(ctx, 0.15)
	appMetrics.checkTxDuration.Record(ctx, 0.05)
	appMetrics.deliverTxDuration.Record(ctx, 0.01)
	appMetrics.deliverBatchTxDuration.Record(ctx, 0.1)
	appMetrics.commitDuration.Record(ctx, 0.3)
	appMetrics.blockProcessDuration.Record(ctx, 0.5)

	appMetrics.txCount.Add(ctx, 1)
	appMetrics.txProcessType.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "sequential")))
	appMetrics.txGas.Add(ctx, 100, otelmetric.WithAttributes(attribute.String("type", "gas_used")))
	appMetrics.txGasUsed.Record(ctx, 1000)
	appMetrics.txGasWanted.Record(ctx, 2000)

	appMetrics.optimisticProcessing.Add(ctx, 1, otelmetric.WithAttributes(attribute.Bool("enabled", true)))
	appMetrics.failedGasWantedCheck.Add(ctx, 1)
	appMetrics.gigaFallback.Add(ctx, 1)

	appMetrics.invarianceDuration.Record(ctx, 0.01)
	appMetrics.invarianceInvalidKey.Add(ctx, 1, otelmetric.WithAttributes(attribute.String("type", "sei")))
	appMetrics.invarianceUnmarshalFail.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("type", "usei"),
		attribute.String("step", "post_block"),
	))
}
