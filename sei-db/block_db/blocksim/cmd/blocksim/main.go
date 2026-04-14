package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sei-protocol/sei-chain/sei-db/block_db/blocksim"
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

func run() error {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}
	config := blocksim.DefaultBlocksimConfig()
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

	bsMetrics := blocksim.NewBlocksimMetrics(ctx, config)

	bs, err := blocksim.NewBlockSim(ctx, config, bsMetrics)
	if err != nil {
		return fmt.Errorf("failed to create blocksim: %w", err)
	}
	defer func() {
		if err := bs.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing blocksim: %v\n", err)
		}
	}()

	metrics.StartMetricsServer(ctx, reg, config.MetricsAddr)
	metrics.StartSystemMetrics(ctx, "blocksim", config.BackgroundMetricsScrapeInterval,
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
					bs.Resume()
					suspended = false
				} else {
					bs.Suspend()
					suspended = true
				}
			}
		}()
	}

	bs.BlockUntilHalted()

	return nil
}
