package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/datalake/lakesim"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// exitOnSecondInterrupt ends the process on the second interrupt it receives. The first interrupt asks the benchmark
// to flush every lane and stop, which can take a while against a slow bucket.
func exitOnSecondInterrupt() {
	interrupts := make(chan os.Signal, 2)
	signal.Notify(interrupts, os.Interrupt)
	go func() {
		<-interrupts
		<-interrupts
		fmt.Fprintf(os.Stderr, "\nSecond interrupt. Exiting without flushing the lanes.\n")
		os.Exit(130)
	}()
}

// run runs the benchmark described by the config file named on the command line, and returns the first error it hit.
func run() (err error) {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}
	config := lakesim.DefaultLakesimConfig()
	if err := utils.LoadConfigFromFile(os.Args[1], config); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	configString, err := utils.StringifyConfig(config)
	if err != nil {
		return fmt.Errorf("stringify config: %w", err)
	}
	fmt.Printf("%s\n", configString)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	exitOnSecondInterrupt()

	registry, shutdown, err := metrics.SetupOtelPrometheus()
	if err != nil {
		return fmt.Errorf("setup metrics: %w", err)
	}
	defer func() {
		_ = shutdown(context.Background())
	}()

	pprofAddr, err := metrics.StartPprofServer(
		ctx, config.PprofAddr, config.MutexProfileFraction, config.BlockProfileRate)
	if err != nil {
		return fmt.Errorf("start pprof server: %w", err)
	}
	if pprofAddr != "" {
		fmt.Printf("pprof listening on %s\n", pprofAddr)
	}
	metrics.StartMetricsServer(ctx, registry, config.MetricsAddr)
	metrics.StartSystemMetrics(ctx, "lakesim", config.BackgroundMetricsScrapeInterval, nil)

	sim, err := lakesim.NewLakesim(ctx, config, lakesim.NewLakesimMetrics())
	if err != nil {
		return fmt.Errorf("start lakesim: %w", err)
	}
	if err := sim.Wait(); err != nil {
		return fmt.Errorf("run lakesim: %w", err)
	}
	return nil
}
