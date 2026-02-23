//go:build slow_bench

package bench

import (
	"os"
	"testing"
)

func BenchmarkLongRunningWrite(b *testing.B) {
	scenario := TestScenario{
		Name:           "long_running_write",
		NumBlocks:      1_000_000_000,
		TotalKeys:      1_000_000_000_000,
		DuplicateRatio: 0.5,
	}

	b.Run(scenario.Name, func(b *testing.B) {
		runBenchmark(b, scenario, true)
	})
}

func BenchmarkLongRunningWriteWithInitialState(b *testing.B) {
	snapshotPath := os.Getenv("SNAPSHOT_PATH")
	if snapshotPath == "" {
		b.Skip("skipping: SNAPSHOT_PATH env var not set")
	}
	scenario := TestScenario{
		SnapshotPath:   snapshotPath,
		Name:           "long_running_write_with_some_initial_state",
		NumBlocks:      1_000_000_000,
		TotalKeys:      1_000_000_000_000,
		DuplicateRatio: 0.5,
	}

	b.Run(scenario.Name, func(b *testing.B) {
		runBenchmark(b, scenario, true)
	})
}
