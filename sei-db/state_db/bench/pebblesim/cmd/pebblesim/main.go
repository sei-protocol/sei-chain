package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/pebblesim"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := pebblesim.DefaultConfig()

	dataDir := flag.String("dir", "./pebblesim-data", "PebbleDB data directory")
	metricsAddr := flag.String("metrics-addr", ":9099", "address to serve Prometheus-format metrics on")
	batchSize := flag.Int("batch-size", cfg.BatchSize, "storage-slot writes per batch")
	interval := flag.Duration("interval", cfg.BatchInterval, "time between batches")
	numContracts := flag.Int("contracts", cfg.NumContracts, "number of simulated contracts")
	slotsPerContract := flag.Int64("slots-per-contract", cfg.SlotsPerContract, "slot index range per contract")
	seed := flag.Int64("seed", cfg.Seed, "random seed")
	flag.Parse()

	cfg.DataDir = *dataDir
	cfg.BatchSize = *batchSize
	cfg.BatchInterval = *interval
	cfg.NumContracts = *numContracts
	cfg.SlotsPerContract = *slotsPerContract
	cfg.Seed = *seed

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	reg, shutdown, err := metrics.SetupOtelPrometheus()
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		_ = shutdown(context.Background())
	}()

	sim, err := pebblesim.Open(cfg)
	if err != nil {
		return fmt.Errorf("open pebblesim: %w", err)
	}
	defer func() {
		if err := sim.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing pebblesim: %v\n", err)
		}
	}()

	metrics.StartMetricsServer(ctx, reg, *metricsAddr)
	metrics.StartSystemMetrics(ctx, "pebblesim", 5, []metrics.MonitoredDir{
		{Name: "data_dir", Path: cfg.DataDir, TrackAvailableSpace: true},
	})

	log.Printf("writing %d storage slots every %s to %s (metrics at http://localhost%s/metrics)",
		cfg.BatchSize, cfg.BatchInterval, cfg.DataDir, *metricsAddr)

	ticker := time.NewTicker(cfg.BatchInterval)
	defer ticker.Stop()

	written := 0
	missed := 0
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			log.Printf("stopping at version %d, %d slots written, %d deadline misses, in %s",
				sim.Version(), written, missed, time.Since(start).Round(time.Second))
			return nil
		case <-ticker.C:
			result, err := sim.WriteBatch()
			if err != nil {
				return fmt.Errorf("write batch: %w", err)
			}
			written += cfg.BatchSize
			generate := (result.Total - result.Write).Round(time.Millisecond)

			if result.Total > cfg.BatchInterval {
				missed++
				log.Printf("version %d: MISSED DEADLINE - total %s (write %s, generate %s), budget %s (%d misses so far)",
					result.Version, result.Total.Round(time.Millisecond), result.Write.Round(time.Millisecond),
					generate, cfg.BatchInterval, missed)
				continue
			}
			log.Printf("version %d: %d slots written in %s (write %s, generate %s) (%d total)",
				result.Version, cfg.BatchSize, result.Total.Round(time.Millisecond), result.Write.Round(time.Millisecond),
				generate, written)
		}
	}
}
