package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
)

// SetupOtelPrometheus configures the global OTel MeterProvider backed by a
// Prometheus registry. Returns the registry (for HTTP serving) and a shutdown
// function that flushes the provider.
func SetupOtelPrometheus() (*prometheus.Registry, func(context.Context) error, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(reg),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	provider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	return reg, provider.Shutdown, nil
}

// StartMetricsServer serves /metrics from the given gatherer on addr.
// If addr is empty the call is a no-op. The server shuts down when ctx is
// cancelled.
func StartMetricsServer(ctx context.Context, gatherer prometheus.Gatherer, addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = srv.ListenAndServe()
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}
