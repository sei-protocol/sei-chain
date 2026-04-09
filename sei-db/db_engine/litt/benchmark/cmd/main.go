package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/benchmark"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: run.sh <config-file-path>\n")
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  run.sh config/basic-config.json\n")
		os.Exit(1)
	}

	reg, shutdown, err := metrics.SetupOtelPrometheus()
	if err != nil {
		log.Fatalf("Failed to set up metrics: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	metrics.StartMetricsServer(context.Background(), reg, ":9101")

	configPath := os.Args[1]

	engine, err := benchmark.NewBenchmarkEngine(configPath)
	if err != nil {
		log.Fatalf("Failed to create benchmark engine: %v", err)
	}

	engine.Logger().Info(fmt.Sprintf("Configuration loaded from %s", configPath))
	engine.Logger().Info("Press Ctrl+C to stop the benchmark")

	err = engine.Run()
	if err != nil {
		engine.Logger().Error(fmt.Sprintf("Benchmark failed: %v", err))
		os.Exit(1)
	} else {
		engine.Logger().Info("Benchmark Terminated")
	}
}
