package main

import (
	"log"

	"github.com/Layr-Labs/eigenda/litt/benchmark"
	"github.com/urfave/cli/v2"
)

// A launcher for the benchmark.
func benchmarkCommand(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return cli.Exit("benchmark command requires exactly one argument: <config-path>", 1)
	}

	configPath := ctx.Args().Get(0)

	// Create the benchmark engine
	engine, err := benchmark.NewBenchmarkEngine(configPath)
	if err != nil {
		log.Fatalf("Failed to create benchmark engine: %v", err)
	}

	// Run the benchmark
	engine.Logger().Infof("Configuration loaded from %s", configPath)
	engine.Logger().Info("Press Ctrl+C to stop the benchmark")

	err = engine.Run()
	if err != nil {
		return err
	} else {
		engine.Logger().Info("Benchmark Terminated")
	}

	return nil
}
