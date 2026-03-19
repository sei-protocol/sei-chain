package metrics

import "github.com/sei-protocol/sei-chain/sei-db/common/unit"

// Shared histogram bucket boundaries for use across the codebase.
// The OTel defaults are too coarse for meaningful percentile queries in Grafana.

// LatencyBuckets covers 10μs to 5 minutes — wide enough for both fast key
// lookups and slow compactions/flushes without needing per-metric tuning.
var LatencyBuckets = []float64{
	0.00001, 0.000025, 0.00005, 0.0001, 0.00025, 0.0005, // 10μs–500μs
	0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, // 1ms–50ms
	0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, // 100ms–5min
}

// ByteSizeBuckets covers 256B to 1GB for data size histograms.
var ByteSizeBuckets = []float64{
	256, unit.KB, 4 * unit.KB, 16 * unit.KB, 64 * unit.KB, 256 * unit.KB,
	unit.MB, 4 * unit.MB, 16 * unit.MB, 64 * unit.MB, 256 * unit.MB, unit.GB,
}

// CountBuckets covers 1 to 1M for per-operation step/iteration counts.
var CountBuckets = []float64{
	1, 5, 10, 50, 100, 500, 1000, 5000, 10000, 100000, 1000000,
}
