# Benchmarks

This package contains benchmarks for the state DB commit store.

## Run benchmarks

From the repo root:

- Standard benchmarks:
  - `go test ./sei-db/state_db/bench -run ^$ -bench . -benchmem`
- Run a single benchmark:
  - `go test ./sei-db/state_db/bench -run ^$ -bench BenchmarkMemIAVLWriteWithDifferentBlockSize -benchmem`

## CI

CI does not measure anything here. `sei-db-tests.yml` runs every benchmark once
with `-benchtime=1x`, which catches a benchmark that no longer runs but produces
no usable timings. Treat a green CI run as "the benchmarks still work", and take
real numbers from a local run on a quiet machine.

A benchmark that cannot run without external infrastructure does not belong in
this package, because that smoke step will fail on it.

## Snapshot pre-population

`TestScenario.SnapshotPath` loads a Cosmos SDK state sync snapshot into the
database before the timed region, so throughput is measured against a
realistically sized tree instead of an empty one. Point it at the directory
holding the numbered chunk files (`0`, `1`, `2`, …), typically
`<node_home>/data/snapshots/<height>/<format>/`.

## Define new scenarios

Benchmarks are configured via `TestScenario`:

- `Name`: scenario name used for the sub-benchmark
- `TotalKeys`: total number of keys to write across all blocks
- `NumBlocks`: number of blocks to commit
- `DuplicateRatio`: fraction of keys that are updates instead of inserts
- `Backend`: database backend (`wrappers.MemIAVL`, `wrappers.FlatKV`,
  `wrappers.CompositeCosmos`, `wrappers.CompositeSplit`, `wrappers.CompositeDual`)
- `Distribution`: per-block key distribution function
- `SnapshotPath`: (optional) path to a state sync snapshot chunks directory;
  when set, the snapshot is imported via the native `Committer.Importer` path
  before the benchmark begins

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

Then set it on a `TestScenario` in `bench_sc_test.go`. A scenario that leaves
`Distribution` unset gets `EvenDistribution`, so a named distribution scenario
that forgets the field silently measures the even case.
