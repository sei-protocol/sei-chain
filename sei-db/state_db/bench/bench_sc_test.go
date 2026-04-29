package bench

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// Using MemIAVL, Tests throughput with fixed total keys,
// varying keysPerBlock and numBlocks to find optimal block size.
func BenchmarkMemIAVLWriteWithDifferentBlockSize(b *testing.B) {
	const totalKeys int64 = 100_000

	scenarios := []TestScenario{
		{
			Name:      "1_key_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys, // 1 key per block
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "2_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2, // 2 keys per block
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "10_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 10, // 10 keys per block
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "20_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 20, // 20 keys per block
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100, // 100 keys per block
			Backend:   wrappers.MemIAVL,
		},
		{
			Name:      "200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200, // 200 keys per block
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

// Using composite backends, tests throughput with fixed total keys,
// varying keysPerBlock and numBlocks. Uses the same reduced scale as FlatKV.
func BenchmarkCompositeWriteWithDifferentBlockSize(b *testing.B) {
	// Note: FlatKV is currently behaving more slowly than expected, and so
	// the total number of keys/blocks is reduced by a factor of 1000 compared to the equivalent MemIAVL benchmarks.
	const totalKeys int64 = 100_000

	scenarios := []TestScenario{
		{
			Name:      "cosmos/100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.CompositeCosmos,
		},
		{
			Name:      "split/100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.CompositeSplit,
		},
		{
			Name:      "dual/100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
			Backend:   wrappers.CompositeDual,
		},
		{
			Name:      "cosmos/200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200,
			Backend:   wrappers.CompositeCosmos,
		},
		{
			Name:      "split/200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200,
			Backend:   wrappers.CompositeSplit,
		},
		{
			Name:      "dual/200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200,
			Backend:   wrappers.CompositeDual,
		},
		{
			Name:      "cosmos/1000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1000,
			Backend:   wrappers.CompositeCosmos,
		},
		{
			Name:      "split/1000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1000,
			Backend:   wrappers.CompositeSplit,
		},
		{
			Name:      "dual/1000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1000,
			Backend:   wrappers.CompositeDual,
		},
		{
			Name:      "cosmos/2000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2000,
			Backend:   wrappers.CompositeCosmos,
		},
		{
			Name:      "split/2000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2000,
			Backend:   wrappers.CompositeSplit,
		},
		{
			Name:      "dual/2000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2000,
			Backend:   wrappers.CompositeDual,
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

// Compares throughput across key distributions with the Composite backend.
func BenchmarkCompositeWriteWithDifferentKeyDistributions(b *testing.B) {
	// Note: FlatKV is currently behaving more slowly than expected, and so
	// the total number of keys/blocks is reduced by a factor of 10 compared to the equivalent MemIAVL benchmarks.
	const (
		totalKeys int64 = 100_000
		numBlocks int64 = 1_000
	)

	scenarios := []TestScenario{
		// Even distribution
		{
			Name:      "cosmos/even_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.CompositeCosmos,
		},
		{
			Name:      "split/even_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.CompositeSplit,
		},
		{
			Name:      "dual/even_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
			Backend:   wrappers.CompositeDual,
		},
		// Bursty distribution
		{
			Name:         "cosmos/bursty_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeCosmos,
			Distribution: BurstyDistribution(1, 10, 5, 3),
		},
		{
			Name:         "split/bursty_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeSplit,
			Distribution: BurstyDistribution(1, 10, 5, 3),
		},
		{
			Name:         "dual/bursty_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeDual,
			Distribution: BurstyDistribution(1, 10, 5, 3),
		},
		// Normal distribution
		{
			Name:         "cosmos/normal_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeCosmos,
			Distribution: NormalDistribution(1, 0.2),
		},
		{
			Name:         "split/normal_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeSplit,
			Distribution: NormalDistribution(1, 0.2),
		},
		{
			Name:         "dual/normal_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeDual,
			Distribution: NormalDistribution(1, 0.2),
		},
		// Ramp distribution
		{
			Name:         "cosmos/ramp_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeCosmos,
			Distribution: RampDistribution(0.5, 1.5),
		},
		{
			Name:         "split/ramp_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeSplit,
			Distribution: RampDistribution(0.5, 1.5),
		},
		{
			Name:         "dual/ramp_distribution",
			TotalKeys:    totalKeys,
			NumBlocks:    numBlocks,
			Backend:      wrappers.CompositeDual,
			Distribution: RampDistribution(0.5, 1.5),
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}
