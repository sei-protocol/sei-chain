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
	batchSize := flag.Int("batch-size", cfg.BatchSize, "key/value writes per batch (split 60/25/15 across slots/balances/nonces)")
	interval := flag.Duration("interval", cfg.BatchInterval, "time between batches")
	numContracts := flag.Int("contracts", cfg.NumContracts, "number of simulated contracts")
	slotsPerContract := flag.Int64("slots-per-contract", cfg.SlotsPerContract, "slot index range per contract")
	queueDepth := flag.Int("queue-depth", cfg.QueueDepth, "batches to buffer ahead of the writer")
	presort := flag.Bool("presort", cfg.Presort, "sort each batch by key on the generator goroutine before it reaches the writer")
	seed := flag.Int64("seed", cfg.Seed, "random seed")
	readsPerSecond := flag.Float64("reads-per-second", cfg.ReadsPerSecond, "combined random-read rate across all read workers; 0 disables reads")
	readWorkers := flag.Int("read-workers", cfg.ReadWorkers, "goroutines issuing random reads against the shared rate limiter")
	readKeyPoolCapacity := flag.Int("read-key-pool-capacity", cfg.ReadKeyPoolCapacity, "number of known-written keys kept available for reads to sample from")
	keepRecent := flag.Int64("keep-recent", cfg.KeepRecent, "number of recent versions the state store retains; older versions become eligible for pruning (0 disables pruning)")
	pruneInterval := flag.Duration("prune-interval", cfg.PruneInterval, "how often the pruning manager checks for prunable versions; only meaningful when keep-recent > 0")
	flag.Parse()

	cfg.DataDir = *dataDir
	cfg.BatchSize = *batchSize
	cfg.BatchInterval = *interval
	cfg.NumContracts = *numContracts
	cfg.SlotsPerContract = *slotsPerContract
	cfg.QueueDepth = *queueDepth
	cfg.Presort = *presort
	cfg.Seed = *seed
	cfg.ReadsPerSecond = *readsPerSecond
	cfg.ReadWorkers = *readWorkers
	cfg.ReadKeyPoolCapacity = *readKeyPoolCapacity
	cfg.KeepRecent = *keepRecent
	cfg.PruneInterval = *pruneInterval

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

	sim.Generate(ctx)
	sim.StartReaders(ctx)
	sim.StartPruning()

	metrics.StartMetricsServer(ctx, reg, *metricsAddr)
	metrics.StartSystemMetrics(ctx, "pebblesim", 5, []metrics.MonitoredDir{
		{Name: "data_dir", Path: cfg.DataDir, TrackAvailableSpace: true},
	})

	pruning := "pruning disabled"
	if cfg.KeepRecent > 0 && cfg.PruneInterval > 0 {
		pruning = fmt.Sprintf("keeping last %d versions, checked every %s", cfg.KeepRecent, cfg.PruneInterval)
	}
	log.Printf("writing %d keys (60%% slots / 25%% balances / 15%% nonces) every %s to %s; reading %.0f/s across %d workers; %s (metrics at http://localhost%s/metrics)",
		cfg.BatchSize, cfg.BatchInterval, cfg.DataDir, cfg.ReadsPerSecond, cfg.ReadWorkers, pruning, *metricsAddr)

	ticker := time.NewTicker(cfg.BatchInterval)
	defer ticker.Stop()

	written := 0
	missed := 0
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			log.Printf("stopping at version %d, %d keys written, %d deadline misses, in %s",
				sim.Version(), written, missed, time.Since(start).Round(time.Second))
			return nil
		case <-ticker.C:
			result, err := sim.WriteBatch(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Shutting down: the ticker case won the race against ctx.Done() above, but
					// there's nothing left to report that the next loop iteration won't.
					continue
				}
				return fmt.Errorf("write batch: %w", err)
			}
			written += cfg.BatchSize

			if result.Total > cfg.BatchInterval {
				missed++
				log.Printf("version %d: MISSED DEADLINE - total %s (write %s, stall %s, sort %s), budget %s (%d misses so far)",
					result.Version, result.Total.Round(time.Millisecond), result.Write.Round(time.Millisecond),
					result.Stall.Round(time.Millisecond), result.Sort.Round(time.Millisecond), cfg.BatchInterval, missed)
				continue
			}
			log.Printf("version %d: %d keys written in %s (write %s, stall %s, sort %s) (%d total)",
				result.Version, cfg.BatchSize, result.Total.Round(time.Millisecond), result.Write.Round(time.Millisecond),
				result.Stall.Round(time.Millisecond), result.Sort.Round(time.Millisecond), written)
		}
	}
}
