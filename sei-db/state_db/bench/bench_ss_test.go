package bench

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func BenchmarkSSPebbleDBWrite(b *testing.B) {
	const totalKeys int64 = 10_000

	scenarios := []TestScenario{
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.SSPebbleDB,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}

func BenchmarkCombinedFlatKVSSPebbleWrite(b *testing.B) {
	const totalKeys int64 = 10_000

	scenarios := []TestScenario{
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.FlatKV_SSPebble,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}
