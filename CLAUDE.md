# Sei Chain

## Code Style

### Go Formatting

All Go files must be `gofmt` compliant. After modifying any `.go` files, run:

```bash
gofmt -s -w <file>
```

Or verify compliance with:

```bash
gofmt -s -l .
```

This command should produce no output if all files are properly formatted.

## Benchmarking

### Single scenario

```bash
GIGA_EXECUTOR=true GIGA_OCC=true DEBUG=true scripts/benchmark.sh
```

TPS is logged every 5s as `tps=<value>` (with ANSI color codes). Extract with:

```bash
grep "tps=" /tmp/sei-benchmark.log | sed 's/\x1b\[[0-9;]*m//g' | sed -n 's/.*tps=\([0-9.]*\).*/\1/p'
```

### Parallel multi-scenario comparison

Use `scripts/benchmark-compare.sh` to run multiple git commits side-by-side:

```bash
scripts/benchmark-compare.sh \
  pre-opt=fd2e28d74 \
  lazy-cms=82acf458d \
  lazy-cms-fix=37a17fd02
```

Each scenario gets its own binary, home dir, and port set (offset by 100). Results are printed at the end with median/avg/min/max TPS. Raw data in `/tmp/sei-bench/<label>/tps.txt`.

Set `DURATION=600` (default) to control how long each run lasts.

### Comparing results across sessions

Cross-session benchmark numbers (TPS, total allocs) are not directly comparable. Only comparisons within the same `benchmark-compare.sh` run are valid, since all scenarios share identical conditions.

### Comparing pprof profiles

Always use `pprof -diff_base` to compare profiles between benchmark runs. Never compare profiles side-by-side manually.

```bash
# CPU diff (positive = regression, negative = improvement)
go tool pprof -top -diff_base /tmp/sei-bench/<baseline>/pprof/cpu.pb.gz /tmp/sei-bench/<candidate>/pprof/cpu.pb.gz

# Allocation diff
go tool pprof -alloc_space -top -diff_base /tmp/sei-bench/<baseline>/pprof/heap.pb.gz /tmp/sei-bench/<candidate>/pprof/heap.pb.gz
```

## PR Workflow

- PRs stack on each other (never target main directly)
- PR descriptions must include pre/post TPS benchmark comparison tables
- All benchmarks run on the same machine — never mention hardware differences between runs
- sei-tendermint CI failures are flaky and should be ignored

## Planning

- Planning docs live in `.planning/`, excluded from git
- Profile artifacts go to `/tmp/sei-profile/`, not the repo
