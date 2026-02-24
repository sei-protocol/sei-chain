//go:build slow_bench

package bench

import (
	"os"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func BenchmarkMemIAVLLongRunningWrite(b *testing.B) {
	scenario := TestScenario{
		Name:           "long_running_write",
		NumBlocks:      1_000_000_000,
		TotalKeys:      1_000_000_000_000,
		DuplicateRatio: 0.5,
		Backend:        wrappers.MemIAVL,
	}

	b.Run(scenario.Name, func(b *testing.B) {
		runBenchmark(b, scenario, true)
	})
}

func BenchmarkFlatKVLongRunningWrite(b *testing.B) {
	scenario := TestScenario{
		Name:           "long_running_write",
		NumBlocks:      1_000_000_000,
		TotalKeys:      1_000_000_000_000,
		DuplicateRatio: 0.5,
		Backend:        wrappers.FlatKV,
	}

	b.Run(scenario.Name, func(b *testing.B) {
		runBenchmark(b, scenario, true)
	})
}

func BenchmarkMemIAVLLongRunningWriteWithInitialState(b *testing.B) {
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
		Backend:        wrappers.MemIAVL,
	}

	b.Run(scenario.Name, func(b *testing.B) {
		runBenchmark(b, scenario, true)
	})
}

func BenchmarkFlatKVLongRunningWriteWithInitialState(b *testing.B) {
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
		Backend:        wrappers.FlatKV,
	}

	b.Run(scenario.Name, func(b *testing.B) {
		runBenchmark(b, scenario, true)
	})
}
