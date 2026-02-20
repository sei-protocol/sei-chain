# Benchmarks

This package contains benchmarks for the state DB commit store.

## Run benchmarks

From the repo root:

- Standard benchmarks:
  - `go test ./sei-db/state_db/bench -run ^$ -bench . -benchmem`
- Run a single benchmark:
  - `go test ./sei-db/state_db/bench -run ^$ -bench BenchmarkMemIAVLWriteWithDifferentBlockSize -benchmem`

## Long running benchmark

The long running benchmark is behind the `slow_bench` build tag and is intended
to run for a long time while you watch the periodic progress report.

- Run it with a long benchtime (interrupt when done):
  - `go test ./sei-db/state_db/bench -run ^$ -bench BenchmarkLongRunningWrite -benchmem -benchtime=24h -tags=slow_bench`

Progress is printed to stdout every few seconds while the benchmark is running.

## Define new scenarios

Benchmarks are configured via `TestScenario`:

- `Name`: scenario name used for the sub-benchmark
- `TotalKeys`: total number of keys to write across all blocks
- `NumBlocks`: number of blocks to commit
- `DuplicateRatio`: fraction of keys that are updates instead of inserts
- `Backend`: database backend (`wrappers.MemIAVL`, `wrappers.FlatKV`,
  `wrappers.CompositeCosmos`, `wrappers.CompositeSplit`, `wrappers.CompositeDual`)
- `Distribution`: per-block key distribution function

Example:

```go
scenario := TestScenario{
	Name:           "bursty_updates",
	TotalKeys:      100_000,
	NumBlocks:      10_000,
	DuplicateRatio: 0.25,
	Distribution:   BurstyDistribution(1, 10, 5, 3),
	Backend:        wrappers.MemIAVL,
}
```

## Add a new distribution

Define a new `KeyDistribution` in `helper.go`:

```go
func MyDistribution(numBlocks, totalKeys, block int64) int64 {
	// return the number of keys for this block
	return totalKeys / numBlocks
}
```

Then set it on a `TestScenario` in `bench_sc_test.go` or
`bench_sc_long_running_test.go`.
