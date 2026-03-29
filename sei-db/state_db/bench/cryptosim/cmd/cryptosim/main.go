package main

import (
	"bufio"
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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/cryptosim"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
)

// setupOtelPrometheus configures the global OTel MeterProvider to export to Prometheus.
// Returns the registry (for HTTP serving) and a shutdown function.
func setupOtelPrometheus() (*prometheus.Registry, func(context.Context) error, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(reg),
		// No namespace: instrument names (e.g. cryptosim_blocks_finalized_total) are used as-is for Grafana compatibility.
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	provider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	return reg, provider.Shutdown, nil
}

// startMetricsServer serves /metrics from the given gatherer. Shuts down when ctx is cancelled.
func startMetricsServer(ctx context.Context, gatherer prometheus.Gatherer, addr string) {
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

// Run the cryptosim benchmark.
func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}
	config, err := cryptosim.LoadConfigFromFile(os.Args[1])
	if err != nil {
		return err
	}

	configString, err := config.StringifiedConfig()
	if err != nil {
		return fmt.Errorf("failed to stringify config: %w", err)
	}
	fmt.Printf("%s\n", configString)

	if config.DeleteDataDirOnStartup {
		if config.DataDir == "" {
			return fmt.Errorf("DataDir is empty, refusing to delete")
		}
		resolved, err := filepath.Abs(config.DataDir)
		if err != nil {
			return fmt.Errorf("failed to resolve data directory: %w", err)
		}
		fmt.Printf("Deleting data directory: %s\n", resolved)
		err = os.RemoveAll(resolved)
		if err != nil {
			return fmt.Errorf("failed to delete data directory: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Configure OTel to export to Prometheus before creating cryptosim (metrics use global provider).
	reg, shutdown, err := setupOtelPrometheus()
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		_ = shutdown(context.Background())
	}()

	cs, err := cryptosim.NewCryptoSim(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create cryptosim: %w", err)
	}
	defer func() {
		err := cs.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error closing cryptosim: %v\n", err)
		}
	}()

	// Start metrics HTTP server after cryptosim setup (metrics are populated).
	startMetricsServer(ctx, reg, config.MetricsAddr)

	// Toggle suspend/resume on Enter when enabled
	if config.EnableSuspension {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			suspended := false
			for scanner.Scan() {
				if suspended {
					cs.Resume()
					suspended = false
				} else {
					cs.Suspend()
					suspended = true
				}
			}
		}()
	}

	cs.BlockUntilHalted()

	if config.DeleteDataDirOnShutdown {
		for _, dir := range []string{config.DataDir, config.LogDir} {
			if dir == "" {
				return fmt.Errorf("directory path is empty, refusing to delete")
			}
			resolved, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("failed to resolve directory: %w", err)
			}
			fmt.Printf("Deleting directory: %s\n", resolved)
			err = os.RemoveAll(resolved)
			if err != nil {
				return fmt.Errorf("failed to delete directory %s: %w", resolved, err)
			}
		}
	}

	return nil
}
