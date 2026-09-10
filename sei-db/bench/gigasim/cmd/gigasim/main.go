package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sei-protocol/sei-chain/sei-db/bench/gigasim"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// run returns the first error the benchmark hit, so that an automated harness sees a failed run in the
// exit code rather than only in the console output.
func run() (err error) {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}
	config := gigasim.DefaultGigasimConfig()
	if err := utils.LoadConfigFromFile(os.Args[1], config); err != nil {
		return err
	}

	configString, err := utils.StringifyConfig(config)
	if err != nil {
		return fmt.Errorf("failed to stringify config: %w", err)
	}
	fmt.Printf("%s\n", configString)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	reg, shutdown, err := metrics.SetupOtelPrometheus()
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		_ = shutdown(context.Background())
	}()

	gsMetrics := gigasim.NewGigasimMetrics()

	gs, err := gigasim.NewGigaSim(ctx, config, gsMetrics)
	if err != nil {
		return fmt.Errorf("failed to create gigasim: %w", err)
	}
	defer func() {
		if closeErr := gs.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	// The server starts after setup so that the rates it reports describe the measured workload rather
	// than the account prepopulation that precedes it.
	metrics.StartMetricsServer(ctx, reg, config.MetricsAddr)
	metrics.StartSystemMetrics(ctx, "gigasim", config.BackgroundMetricsScrapeInterval,
		[]metrics.MonitoredDir{
			{Name: "data_dir", Path: config.DataDir, TrackAvailableSpace: true},
			{Name: "log_dir", Path: config.LogDir},
		})

	if config.EnableSuspension {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			suspended := false
			for scanner.Scan() {
				if suspended {
					gs.Resume()
					suspended = false
				} else {
					gs.Suspend()
					suspended = true
				}
			}
		}()
	}

	gs.BlockUntilHalted()

	return nil
}
