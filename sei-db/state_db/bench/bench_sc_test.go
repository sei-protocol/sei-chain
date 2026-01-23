package bench

import "testing"

// BenchmarkWriteWithDifferentBlockSize tests throughput with fixed 1M total keys,
// varying keysPerBlock and numBlocks to find optimal block size.
func BenchmarkWriteWithDifferentBlockSize(b *testing.B) {
	const totalKeys int64 = 1_000_000

	scenarios := []TestScenario{
		{
			Name:      "1_key_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1,
		},
		{
			Name:      "2_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2,
		},
		{
			Name:      "10_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 10,
		},
		{
			Name:      "20_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 20,
		},
		{
			Name:      "100_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 100,
		},
		{
			Name:      "200_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 200,
		},
		{
			Name:      "1000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 1000,
		},
		{
			Name:      "2000_keys_per_block",
			TotalKeys: totalKeys,
			NumBlocks: totalKeys / 2000,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}

// BenchmarkWriteWithDifferentKeyDistributions compares throughput across key distributions.
func BenchmarkWriteWithDifferentKeyDistributions(b *testing.B) {
	const (
		totalKeys int64 = 1_000_000
		numBlocks int64 = 10_000
	)

	scenarios := []TestScenario{
		{
			Name:      "even_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
		},
		{
			Name:      "bursty_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
		},
		{
			Name:      "normal_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
		},
		{
			Name:      "ramp_distribution",
			TotalKeys: totalKeys,
			NumBlocks: numBlocks,
		},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.Name, func(b *testing.B) {
			runBenchmark(b, scenario, false)
		})
	}
}
