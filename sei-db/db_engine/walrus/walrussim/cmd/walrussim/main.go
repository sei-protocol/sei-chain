package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus/walrussim"
)

// setupOtelPrometheus points the global OTel meter provider at a Prometheus registry.
func setupOtelPrometheus() (*prometheus.Registry, func(context.Context) error, error) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// No namespace: instrument names such as walrus_pods_probed are used verbatim, so a Grafana query written
	// against one run works against another.
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create the prometheus exporter: %w", err)
	}

	provider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)
	return registry, provider.Shutdown, nil
}

// startMetricsServer serves /metrics until the context is cancelled.
func startMetricsServer(runContext context.Context, gatherer prometheus.Gatherer, address string) {
	if address == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{}))
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() { _ = server.ListenAndServe() }()
	go func() {
		<-runContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
}

// Run the walrussim benchmark.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [config-file]\n", os.Args[0])
		os.Exit(1)
	}

	// A config file states only what it changes. With none, the defaults run as they are.
	config := walrussim.DefaultConfig()
	if len(os.Args) == 2 {
		loaded, err := walrussim.LoadConfigFromFile(os.Args[1])
		if err != nil {
			return err
		}
		config = loaded
	}
	configString, err := config.StringifiedConfig()
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", configString)

	if config.DeleteDataDirOnStartup {
		resolved, err := filepath.Abs(config.DataDir)
		if err != nil {
			return fmt.Errorf("failed to resolve the data directory: %w", err)
		}
		fmt.Printf("Deleting data directory: %s\n", resolved)
		if err := os.RemoveAll(resolved); err != nil {
			return fmt.Errorf("failed to delete the data directory: %w", err)
		}
	}

	runContext, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The meter provider has to be in place before the simulator is built, since its instruments are created
	// against whatever provider is global at the time.
	registry, shutdown, err := setupOtelPrometheus()
	if err != nil {
		return err
	}
	defer func() { _ = shutdown(context.Background()) }()

	simulator, err := walrussim.NewWalrusSim(runContext, config)
	if err != nil {
		return err
	}
	defer func() {
		if err := simulator.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing walrussim: %v\n", err)
		}
	}()

	startMetricsServer(runContext, registry, config.MetricsAddr)
	return simulator.Run()
}
