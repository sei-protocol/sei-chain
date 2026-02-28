package bench

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func BenchmarkSSCompositeWrite(b *testing.B) {
	const totalKeys int64 = 10_000

	scenarios := []TestScenario{
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.SSComposite,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}

func BenchmarkCombinedCompositeDualSSCompositeWrite(b *testing.B) {
	const totalKeys int64 = 10_000

	scenarios := []TestScenario{
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.CompositeDual_SSComposite,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}
