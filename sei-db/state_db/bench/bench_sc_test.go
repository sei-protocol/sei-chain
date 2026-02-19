package bench

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// Using MemIAVL, Tests throughput with fixed total keys,
// varying keysPerBlock and numBlocks to find optimal block size.
func BenchmarkMemIAVLWriteWithDifferentBlockSize(b *testing.B) {
	const totalKeys int64 = 1_000_000

	scenarios := []TestScenario{
		{
			Name:      "1_key_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "2_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "10_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 10,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "20_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 20,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "1000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1000,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "2000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2000,
			Backend:   wrappers.MemIAVL,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}

// Using FlatKV, Tests throughput with fixed total keys,
// varying keysPerBlock and numBlocks to find optimal block size.
func BenchmarkFlatKVWriteWithDifferentBlockSize(b *testing.B) {
	// Note: FlatKV is currently behaving more slowly than expected, and so
	// the total number of keys/blocks is reduced by a factor of 1000 compared to the equivalent MemIAVL benchmarks.
	const totalKeys int64 = 100_000

	scenarios := []TestScenario{
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.FlatKV,
		},
		{
			Name:      "200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200,
			Backend:   wrappers.FlatKV,
		},
		{
			Name:      "1000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1000,
			Backend:   wrappers.FlatKV,
		},
		{
			Name:      "2000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2000,
			Backend:   wrappers.FlatKV,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}

// Compares throughput across key distributions with The MemIAVL backend.
func BenchmarkMemIAVLWriteWithDifferentKeyDistributions(b *testing.B) {
	const (
		totalKeys int64 = 1_000_000
		numBlocks int64 = 10_000
	)

	scenarios := []TestScenario{
		{
			Name:      "even_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "bursty_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "normal_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "ramp_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.MemIAVL,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}

// Compares throughput across key distributions with The FlatKV backend.
func BenchmarkFlatKVWriteWithDifferentKeyDistributions(b *testing.B) {
	// Note: FlatKV is currently behaving more slowly than expected, and so
	// the total number of keys/blocks is reduced by a factor of 10 compared to the equivalent MemIAVL benchmarks.
	const (
		totalKeys int64 = 100_000
		numBlocks int64 = 1_000
	)

	scenarios := []TestScenario{
		{
			Name:      "even_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.FlatKV,
		},
		{
			Name:      "bursty_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.FlatKV,
		},
		{
			Name:      "normal_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.FlatKV,
		},
		{
			Name:      "ramp_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.FlatKV,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}
