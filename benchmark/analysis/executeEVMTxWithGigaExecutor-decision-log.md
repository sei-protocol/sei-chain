# Decision Log: executeEVMTxWithGigaExecutor Optimization Direction

## Date

2026-02-13

## Objective

Pick a concrete optimization direction for `executeEVMTxWithGigaExecutor` and validate whether it produces a meaningful TPS gain before implementing changes.

## Status update

- The sync.Map-to-plain-map cachekv change (`0244baf53...`, previously recorded as `87c97e7b2...` in earlier notes) is now formally closed.
- The branch now contains commit `a8e96318d` reverting that change.
- Reason: 300s benchmark evidence remained within noise (`~+0.5%` to `~+0.7%`, high variance, mixed deltas), so we did not retain the optimization in-tree.
- Current active code has returned to the previous sync.Map state with pool-based optimization already reverted.

### Pool-change history reference (preserved)

- The earlier pool experiment commit is `dbd6ad52ff10e003bed8ba2b5744f39b867fb876`.
- It was reverted by `ebddcb9cea7d8bf2696911c49dcb11023a0350fb`.
- This run keeps `dbd6...` reverted and evaluates the later map-based candidate against that reverted baseline.

### What was actually optimized in this run

- The comparison targeted one concrete code change: commit `0244baf53...` (`giga/deps/store/cachekv.go`) changed from `sync.Map` + `sync.Map`-based deleted tracking to regular maps (`map[string]*types.CValue` and `map[string]struct{}`), with mutex protection added around map access.
- The baseline commit `ebddcb9cea7d8bf2696911c49dcb11023a0350fb` is the direct predecessor where `cachekv.Store` pooling (`sync.Pool`) was reverted, so this run specifically tests the map-vs-pool design for giga cachekv in the existing snapshot/cache-heavy execution path.
- No additional optimization passes were applied during this validation pass beyond the above candidate-vs-baseline diff.

### Closure decision

- `0244baf53...` is no longer part of this branch; it was reverted by `a8e96318d` after the validation pass due insufficient signal.

## What we committed to in this validation pass

- **Approach chosen:** validate candidate vs baseline through a statistically informed benchmark pass, not implementation.
- **Scope kept constant:**
  - scenario: `benchmark/scenarios/evm.json`
  - flags: `GIGA_EXECUTOR=true GIGA_OCC=true BENCHMARK_TXS_PER_BATCH=1000`
  - duration: `DURATION=300`
  - artifacts: TPS + `pprof/{cpu,fgprof,heap,goroutine,block,mutex}` for both branches

## Candidate selected for check

- Baseline: `ebddcb9cea7d8bf2696911c49dcb11023a0350fb`
- Candidate: `0244baf53...`

## Method used

1. Run `benchmark/benchmark-compare.sh baseline=<baseline> candidate=<candidate>`.
2. Capture paired TPS series from `/tmp/sei-bench/baseline/tps.txt` and `/tmp/sei-bench/candidate/tps.txt`.
3. Compute paired deltas and t-stat to avoid overreading one-run medians.
4. Generate focused profile diffs for the execute function path:
   - `cpu.pb.gz` (cum and focus)
   - `fgprof.pb.gz` (cum and focus)
   - `heap.pb.gz` (alloc-space cum diff)

## Execution result

- Run300 outcomes:
  - baseline median/avg: `6200 / 6116`
  - candidate median/avg: `6200 / 6155`
  - paired mean delta: `+40.4`
  - non-positive deltas: `35.0%`
  - t-stat: `~0.377`
- Run301 outcomes:
  - baseline median/avg: `6479 / 6404`
  - candidate median/avg: `6599 / 6436`
  - paired mean delta: `+27.2`
  - non-positive deltas: `45.8%`
  - t-stat: `~0.180`

### Decision

- **No clear sustained optimization win was proven**.
- Mean TPS lift looked small and noisy (`~+0.5%` to `~+0.7%`), with mixed per-interval direction and high variance.
- The result is **not a go/no-go signal** for code changes yet.

### Profile outcome

- Focused diffs on `executeEVMTxWithGigaExecutor` were mixed and low-magnitude overall.
- No single, obvious path improvement dominated CPU/fgprof deltas.
- Heap diffs showed meaningful churn in `sync.Map`/`HashTrieMap` and store-write internals (`newEntryNode`, `newIndirectNode`, `cachekv`, `cachemulti`), consistent with prior hotspot theory but still not resolved into a stable net gain.

## Artifacts captured this pass

- `/tmp/sei-bench-rep-run300.log`
- `/tmp/sei-bench-rep-run301.log`
- `/tmp/sei-bench/baseline/tps.txt`
- `/tmp/sei-bench/candidate/tps.txt`
- `/tmp/sei-bench/baseline/pprof/{cpu,fgprof,heap,goroutine,block,mutex}.pb.gz`
- `/tmp/sei-bench/candidate/pprof/{cpu,fgprof,heap,goroutine,block,mutex}.pb.gz`
- `/tmp/candidate_cpu_focus_execute_301.txt`
- `/tmp/candidate_cpu_cum_executeevm_301.txt`
- `/tmp/candidate_fgprof_cum_executeevm_301.txt`
- `/tmp/candidate_heap_alloc_cum_executeevm_301.txt`

## Next-direction guidance (to avoid redoing this iteration)

Before the next benchmark run, define one **single** optimization direction and a pass/fail rule:

- **Direction A (snapshot/store allocation reduction):** implement one proposed change, then require at least 2 fresh `DURATION=300` compare runs where paired mean delta is positive and statistically stronger than noise.
- **Direction B (sync.Map alternatives):** benchmark after implementation with same flags and scripts.
- **Direction C (small constant-caching tweaks):** run as a lower-risk follow-up only after A/B are validated, since expected gain is small.

### Suggested acceptance rule per direction

- minimum 2 runs
- consistent median/average gain direction
- nonpositive paired deltas should be materially below 25%
- clear improvement in focused `executeEVMTxWithGigaExecutor` path in either CPU cum or fgprof cum profile
- no regressions in lock, goroutine, or mutex profiles that exceed previous baseline risk thresholds
