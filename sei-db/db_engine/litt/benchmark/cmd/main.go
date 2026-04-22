package main

import (
	"fmt"
	"log"
	"os"

	"github.com/Layr-Labs/eigenda/litt/benchmark"
)

func main() {
	// Check for required argument
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: run.sh <config-file-path>\n")
		_, _ = fmt.Fprintf(os.Stderr, "\nExample:\n")
		_, _ = fmt.Fprintf(os.Stderr, "  run.sh config/basic-config.json\n")
		os.Exit(1)
	}

	configPath := os.Args[1]

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
		engine.Logger().Fatalf("Benchmark failed: %v", err)
	} else {
		engine.Logger().Info("Benchmark Terminated")
	}
}
