//go:build slow_bench

package bench

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func BenchmarkLongRunningWrite(b *testing.B) {
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
