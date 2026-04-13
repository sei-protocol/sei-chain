package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
)

const loadtestTelemetryServiceName = "loadtest-client"

var (
	loadtestOtelErr error

	loadtestPromReg    *prometheus.Registry
	loadtestShutdownMP func(context.Context) error

	loadtestProduceCounter metric.Int64Counter
	loadtestConsumeCounter metric.Int64Counter
	loadtestTpsGauge       metric.Float64Gauge

	loadtestHost string
)

func loadtestMetricOpts(msgType string) metric.MeasurementOption {
	var once sync.Once
	once.Do(func() {
		loadtestHost, _ = os.Hostname()
		if loadtestHost == "" {
			loadtestHost = "unknown"
		}
	})
	return metric.WithAttributes(
		attribute.String("msg_type", msgType),
		attribute.String("service", loadtestTelemetryServiceName),
		attribute.String("host", loadtestHost),
	)
}

// initLoadtestOtelMetrics configures the process-wide OTel MeterProvider and Prometheus
// registry used by the loadtest metrics HTTP server. Call once from run() before any
// producer/consumer records metrics.
func initLoadtestOtelMetrics() (func(context.Context) error, error) {
	var once sync.Once
	once.Do(func() {
		loadtestPromReg = prometheus.NewRegistry()

		exporter, err := otelprometheus.New(
			otelprometheus.WithRegisterer(loadtestPromReg),
			otelprometheus.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithoutSuffixes),
			otelprometheus.WithoutTargetInfo(),
			otelprometheus.WithoutScopeInfo(),
		)
		if err != nil {
			loadtestOtelErr = fmt.Errorf("create otel prometheus exporter: %w", err)
			return
		}

		provider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
		otel.SetMeterProvider(provider)

		meter := provider.Meter("loadtest")

		loadtestProduceCounter, loadtestOtelErr = meter.Int64Counter("sei_loadtest_produce_count")
		if loadtestOtelErr != nil {
			loadtestOtelErr = fmt.Errorf("produce counter: %w", loadtestOtelErr)
			return
		}
		loadtestConsumeCounter, loadtestOtelErr = meter.Int64Counter("sei_loadtest_consume_count")
		if loadtestOtelErr != nil {
			loadtestOtelErr = fmt.Errorf("consume counter: %w", loadtestOtelErr)
			return
		}
		loadtestTpsGauge, loadtestOtelErr = meter.Float64Gauge("sei_loadtest_tps_tps")
		if loadtestOtelErr != nil {
			loadtestOtelErr = fmt.Errorf("tps gauge: %w", loadtestOtelErr)
			return
		}

		loadtestShutdownMP = provider.Shutdown
	})

	if loadtestOtelErr != nil {
		return nil, loadtestOtelErr
	}
	if loadtestShutdownMP == nil {
		return nil, fmt.Errorf("loadtest otel metrics: shutdown unset")
	}
	return loadtestShutdownMP, nil
}

func incrProducerEventCount(msgType string) {
	ctx := context.Background()
	loadtestProduceCounter.Add(ctx, 1, loadtestMetricOpts(msgType))
}

func incrConsumerEventCount(msgType string) {
	ctx := context.Background()
	loadtestConsumeCounter.Add(ctx, 1, loadtestMetricOpts(msgType))
}

func setThroughputMetricByType(metricName string, value float32, msgType string) {
	ctx := context.Background()
	// Legacy keys were sei, loadtest, tps, <metricName>. Loadtest only passes "tps".
	if metricName != "tps" {
		return
	}
	loadtestTpsGauge.Record(ctx, float64(value), loadtestMetricOpts(msgType))
}

func loadtestPrometheusGatherer() prometheus.Gatherer {
	return loadtestPromReg
}
