package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof" //nolint:gosec // the profiling endpoint is the point; it is opt-in via PprofAddr
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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

// startPprofServer serves the pprof endpoints and enables the mutex and block profiles at the
// configured sample rates. It returns the address it bound, or "" when PprofAddr is empty. Shuts
// down when ctx is cancelled.
//
// Binding happens before this returns, so a port already in use is an error here rather than silence
// at the far end of an ssh tunnel.
//
// The server has no write timeout: a CPU or trace profile holds its response open for the length of
// the collection, which a timeout would truncate.
func startPprofServer(ctx context.Context, config *cryptosim.CryptoSimConfig) (string, error) {
	if config.PprofAddr == "" {
		return "", nil
	}

	// Off unless asked for, because sampling either one charges the events it samples.
	if config.MutexProfileFraction > 0 {
		runtime.SetMutexProfileFraction(config.MutexProfileFraction)
	}
	if config.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(config.BlockProfileRate)
	}

	listener, err := net.Listen("tcp", config.PprofAddr)
	if err != nil {
		return "", fmt.Errorf("listen on pprof address %q: %w", config.PprofAddr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		_ = srv.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	return listener.Addr().String(), nil
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

	// Before setup rather than after, so that setup is profilable too.
	pprofAddr, err := startPprofServer(ctx, config)
	if err != nil {
		return fmt.Errorf("start pprof server: %w", err)
	}
	if pprofAddr != "" {
		fmt.Printf("pprof listening on %s\n", pprofAddr)
	}

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
		if config.DataDir == "" {
			return fmt.Errorf("DataDir is empty, refusing to delete")
		}
		resolved, err := filepath.Abs(config.DataDir)
		if err != nil {
			return fmt.Errorf("failed to resolve data directory: %w", err)
		}
		fmt.Printf("Deleting data directory: %s\n", resolved)
		if err := os.RemoveAll(resolved); err != nil {
			return fmt.Errorf("failed to delete data directory %s: %w", resolved, err)
		}
	}

	if config.DeleteLogDirOnShutdown {
		if config.LogDir == "" {
			return fmt.Errorf("LogDir is empty, refusing to delete")
		}
		resolved, err := filepath.Abs(config.LogDir)
		if err != nil {
			return fmt.Errorf("failed to resolve log directory: %w", err)
		}
		fmt.Printf("Deleting log directory: %s\n", resolved)
		if err := os.RemoveAll(resolved); err != nil {
			return fmt.Errorf("failed to delete log directory %s: %w", resolved, err)
		}
	}

	return nil
}
