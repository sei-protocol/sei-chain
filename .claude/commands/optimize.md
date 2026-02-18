---
name: optimize
description: Run a profiling-driven optimization loop for a specific function
argument-hint: "<function-name> e.g. executeEVMTxWithGigaExecutor"
allowed-tools:
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - Bash
  - Task
  - AskUserQuestion
---

# Optimization Loop for: $ARGUMENTS

You are running a profiling-driven optimization loop focused on the function `$ARGUMENTS`.

## References

Read `benchmark/CLAUDE.md` for benchmark commands, environment variables, profiling, and the full optimization loop steps.

## Workflow

Execute the optimization loop from benchmark/CLAUDE.md section "Optimization loop", but focused on `$ARGUMENTS`:

### Phase 1: Understand the target function

1. Find the function `$ARGUMENTS` in the codebase using Grep
2. Read the function and its callers/callees to understand the hot path
3. Identify what packages, types, and helpers it uses

### Phase 2: Profile

4. Run the benchmark: `GIGA_EXECUTOR=true GIGA_OCC=true benchmark/benchmark.sh`
5. Wait for it to complete (default DURATION=120s)

### Phase 3: Analyze (focused on target function)

6. Run pprof analysis focused on `$ARGUMENTS` and its call tree. Run these in parallel:
   - CPU: `go tool pprof -top -cum -nodecount=40 /tmp/sei-bench/pprof/cpu.pb.gz 2>&1 | head -60`
   - fgprof: `go tool pprof -top -cum -nodecount=40 /tmp/sei-bench/pprof/fgprof.pb.gz 2>&1 | head -60`
   - Heap (alloc_space): `go tool pprof -alloc_space -top -cum -nodecount=40 /tmp/sei-bench/pprof/heap.pb.gz 2>&1 | head -60`
   - Heap (alloc_objects): `go tool pprof -alloc_objects -top -cum -nodecount=40 /tmp/sei-bench/pprof/heap.pb.gz 2>&1 | head -60`
   - Block: `go tool pprof -top -cum -nodecount=40 /tmp/sei-bench/pprof/block.pb.gz 2>&1 | head -60`
   - Mutex: `go tool pprof -top -cum -nodecount=40 /tmp/sei-bench/pprof/mutex.pb.gz 2>&1 | head -60`
7. Use `go tool pprof -text -focus='$ARGUMENTS' /tmp/sei-bench/pprof/cpu.pb.gz` to get function-focused breakdown
8. Open flamegraphs on separate ports for the user to inspect:
   - `go tool pprof -http=:8080 /tmp/sei-bench/pprof/cpu.pb.gz &`
   - `go tool pprof -http=:8081 /tmp/sei-bench/pprof/fgprof.pb.gz &`
   - `go tool pprof -http=:8082 -alloc_space /tmp/sei-bench/pprof/heap.pb.gz &`

### Phase 4: Summarize and discuss

9. Present findings to the user:
   - TPS from the benchmark run (extract from `/tmp/sei-bench/tps.txt`)
   - Where `$ARGUMENTS` and its callees spend the most time (CPU, wall-clock)
   - Biggest allocation hotspots within the function's call tree
   - Any contention (block/mutex) in the function's path
   - Top 2-3 candidate optimizations with expected impact and trade-offs
10. Ask the user which optimization direction to pursue. Do NOT write any code until the user picks.

### Phase 5: Implement

11. Implement the chosen optimization
12. Run `gofmt -s -w` on all modified `.go` files
13. Commit the change

### Phase 6: Compare

14. Record the commit hash before and after the optimization
15. Run comparison: `benchmark/benchmark-compare.sh baseline=<before-commit> candidate=<after-commit>`
16. Open diff flamegraphs for the user:
    - `go tool pprof -http=:8083 -diff_base /tmp/sei-bench/baseline/pprof/cpu.pb.gz /tmp/sei-bench/candidate/pprof/cpu.pb.gz &`
    - `go tool pprof -http=:8084 -diff_base /tmp/sei-bench/baseline/pprof/fgprof.pb.gz /tmp/sei-bench/candidate/pprof/fgprof.pb.gz &`
    - `go tool pprof -http=:8085 -diff_base /tmp/sei-bench/baseline/pprof/heap.pb.gz /tmp/sei-bench/candidate/pprof/heap.pb.gz &`

### Phase 7: Validate

17. Present results:
    - TPS delta (baseline vs candidate)
    - CPU diff: `go tool pprof -top -diff_base /tmp/sei-bench/baseline/pprof/cpu.pb.gz /tmp/sei-bench/candidate/pprof/cpu.pb.gz`
    - Heap diff: `go tool pprof -alloc_space -top -diff_base /tmp/sei-bench/baseline/pprof/heap.pb.gz /tmp/sei-bench/candidate/pprof/heap.pb.gz`
18. Ask the user: keep, iterate, or revert?
19. If keep and user approves, ask whether to open a PR

## Important rules

- ALWAYS ask the user before writing any optimization code (step 10)
- ALWAYS ask the user before opening a PR (step 19)
- Cross-session benchmark numbers are NOT comparable. Only compare within the same `benchmark-compare.sh` run.
- Run `gofmt -s -w` on all modified Go files before committing
- If `$ARGUMENTS` is empty or not found, ask the user to provide the function name
- GC tuning (GOGC, GOMEMLIMIT, debug.SetGCPercent, debug.SetMemoryLimit) is NOT a valid optimization. Do not modify GC parameters or memory limits. Focus on reducing allocations and improving algorithmic efficiency instead.
