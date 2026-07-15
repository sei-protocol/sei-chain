# gasbench

Differential microbenchmark harness that measures per-opcode EVM execution
time and correlates it with gas cost. Built to unblock the gas-repricing
project's F2 hypothesis test: does gas cost track execution time.

Design: `designs/gas-repricing-telemetry/gas-vs-time-instrumentation.md` in
the `bdchatham-designs` repo. Noise-floor calibration behind the acceptance
gate below: `designs/gas-repricing-telemetry/research/microbenchmark-noise-isolation-tradeoffs.md`
in the same repo.

## The load-bearing invariant: never measure gas and time in the same run

Attaching a `tracing.Hooks` tracer to sum per-opcode gas sets `debug=true` in
the interpreter loop, which dilates per-step execution time. So:

- **Time** comes from a tracer-free run (`vm.Config{}`, nil `Tracer` —
  production's own hot path).
- **Gas** is deterministic and comes from a separate, tracer-free run too
  (`GasLimit - leftOverGas` via `runtime.Call`, not `runtime.Execute`, which
  discards `leftOverGas`).

Both series in a `Case` (baseline and target) are measured this way, so
gas and time are never read off the same call.

## Differential construction

Each `Case` (`programs.go`) is a baseline/target bytecode pair, identical
except the target executes the opcode under test. Everything the two
programs share — loop overhead, setup, dispatch cost — cancels when
`Subtract` (`diff.go`) takes the difference; what remains is attributable to
the opcode.

The repeating unit is balanced so gas is net-zero and the stack never grows:
for an opcode consuming `n` operands and producing 1 result,

```
target   = DUP1 x n , OP     , POP        // dup n copies, run op, drop result
baseline = DUP1 x n , POP x n              // dup n copies, drop them
```

The `n` DUP1s cancel, one POP cancels the target's trailing POP, so the
per-unit gas delta is `ConstGas(op) - (n-1)*GasQuickStep`. Two opcode
families are special-cased in `BuildCaseWith`:

- **Stack ops** (`DUP1`, `SWAP1`) aren't "n operands → 1 result" — each gets
  its own construction.
- **SHL/SHR/SAR** need two *distinct* operands, not `n` copies of the same
  value: a shift amount ≥256 takes go-ethereum's `value.Clear()` early-out
  (the cheapest possible case), so reusing one seed for both operands would
  measure that early-out instead of the real limb-shift path. `seedShift`
  keeps the shift amount in-range and distinct from the value operand.

`EXP` and anything with dynamic/memory/state gas is out of scope — see
`OpSpec.DataDependent` and the design's Non-goals.

## Gas isolation from the Cosmos layer

`Program.Run` builds a bare go-ethereum EVM (`runtime.Call`) with no ante
handler and no Sei `GasMeter`. `gasUsed` is pure EVM gas
(`cfg.GasLimit - leftOverGas`); there is deliberately no Cosmos gas meter in
this path. Correlate against **EVM** gas, not the Cosmos gas meter — mixing
the two units was the CON-368 trap (`msg_server.go:152`).

## Program reuse across calls (journal growth)

A `Program` (`program.go`) is built once per bytecode input and its `Run`
method is called once per timed iteration, reusing one `StateDB` — rebuilding
state per call would swamp a nanosecond-scale signal with allocation noise.
Each call takes a `Snapshot()` that's never reverted or `Finalise`-d, so the
journal's revision/touch-change list grows roughly two entries per call over
a `Program`'s life. This is harmless for the current MVP: growth is
symmetric between baseline and target (both take one snapshot + one
zero-value transfer per call), gas accounting is unaffected (it comes only
from interpreter opcodes), and the cost lands as periodic tail latency from
slice reallocation, not a shift in the median the difference-of-medians
estimator reads. **Re-check this before reusing a `Program` across an
order-of-magnitude more iterations, or for a `Case` that touches persistent
state (SSTORE/LOG/CREATE).**

This periodic reallocation is also the one CoV-inflating noise source the
`getrusage` diagnostics below structurally cannot see (it's not a scheduler
preemption).

## Error contract

`Program.Run` returns `err == nil` only on a clean STOP/RETURN. `REVERT`,
out-of-gas, and invalid-opcode all return a non-nil `err` (see the doc
comment on `Run` for the specific sentinel per case). Every `Case` built by
`BuildCaseWith` terminates cleanly, so a non-nil `err` during measurement
means the run is invalid — `Measure` (`gasbench.go`) propagates it as a hard
error rather than recording a bogus sample.

## Measurement methodology

`Measure` (`gasbench.go`) runs `Warmup` discarded iterations (warm
I-cache/D-cache/branch predictor, catch a broken program early), then
`Iterations` timed iterations of `RunOnce`, config in `Config`/`DefaultConfig`:

- GC disabled for the timed window (a GC pause inside a sample corrupts the
  tail) — the heap grows unbounded during the window, so keep the program
  allocation-light or lower `Iterations`.
- The measuring goroutine is locked to its OS thread and `GOMAXPROCS` should
  be 1, so nothing migrates the goroutine across cores mid-window.
- Timing uses `time.Now`/`time.Since` (monotonic, immune to NTP steps)
  around the tracer-free `RunOnce` call.

## Acceptance gate: Significant, not CoV

`Diff.Significant` (`diff.go`) — `|DeltaNs| > SigmaK * Uncertainty`, where
`Uncertainty` is the two series' median standard errors propagated in
quadrature — is the acceptance gate. A raw per-series CoV of several percent
under plain core-pinning (no kernel-level isolcpus/nohz_full/rcu_nocbs) is
expected physics (the periodic scheduler tick, device IRQs, shared-cache
contention that pinning alone can't remove), not a defect. No major
benchmarking harness (JMH, criterion.rs, Go's own `benchstat`) gates on a
fixed dispersion threshold either — all three gate on effect size against
the estimate's own uncertainty, same as `Significant`.

`Diff.HighVariance` (CoV above `CoVCeiling`, default 0.25) is advisory only:
it flags a run worth a closer look (a noisy neighbor, a throttling event) but
never overrides `Significant`. The 0.25 default sits above the 4-8% CoV
measured as normal on a dedicated, pinned, bare-metal host with no
kernel-level isolation, and well below the ~40%+ territory a genuinely
pathological run would show. See the research doc linked above for the full
sweep (JMH/criterion.rs/benchstat comparison, isolcpus vs. taskset, the
practitioner consensus that isolcpus-level isolation is real-time/HFT-grade
overkill for this class of measurement) and for why the harness deliberately
does *not* add cpuset/IRQ-affinity isolation beyond `run.sh`'s taskset pin.

## Active-benchmarking diagnostics: Nvcsw/Nivcsw

`Series.NvcswDelta`/`NivcswDelta` (`gasbench.go`) are `getrusage(2)`
voluntary/involuntary context-switch counts over the timed window
(`RUSAGE_SELF` — process-wide, not thread-scoped; Go's `syscall` package
exposes no portable `RUSAGE_THREAD`). A nonzero `NivcswDelta` is direct,
measured evidence the kernel scheduler preempted a thread in this process
mid-window — Brendan Gregg's "active benchmarking" check, automated instead
of a one-off manual `perf stat`/`/proc/interrupts` session. It does not cover
every noise source: a high CoV with `NivcswDelta` near zero points at a
non-scheduler tail instead (the journal-growth reallocation above is the
known example). Near-zero `NvcswDelta` confirms the loop stayed on-CPU
without blocking, as expected for a pure-compute loop.

`Diff` surfaces both as `BaselineNvcsw`/`TargetNvcsw` and
`BaselineNivcsw`/`TargetNivcsw`; `Run` surfaces the worse (max) of each pair
as `Nvcsw`/`Nivcsw`. A zero in either reflects an undisturbed window *or* a
failed `getrusage` call — check `Series.Warnings` (surfaced per-opcode via
`b.Logf` in `bench_test.go`) to tell the two apart.

## Running it

`run.sh` pins one measurement thread to one core (`GASBENCH_CORE`, default
3) and checks (but cannot enforce) turbo/governor/isolation settings — see
its header comment for the operator checklist. It runs:

```
go test ./tools/gasbench/ -run '^$' -bench '^BenchmarkOpcodes$' \
  -benchtime=1x -count=10 -benchmem
```

`-benchtime=1x` matters: `BenchmarkOpcodes`'s inner timing loop is
`Measure`'s, not `b.N`'s, so `b.N` must stay at 1. `-count=K` reruns the
benchmark K times **within the same OS process**, not as K independent
process forks — it gives `benchstat`-style cross-run variance, not
process-level independence. True process-level independence would need K
separate `go test` invocations. See the research doc for why this matters
less than it sounds: JMH's fork/warmup/measurement-iteration model is the
gold standard, but this MVP's within-process reruns already surfaced
reproducible per-opcode deltas (see the research doc's empirical run).

Env vars: `GASBENCH_WARMUP`, `GASBENCH_ITERS` (override `DefaultConfig`),
`GASBENCH_SIGMA_K` (default 3), `GASBENCH_COV_CEILING` (default 0.25),
`GASBENCH_COUNT` (default 10), `GASBENCH_OUT_CSV`, `GASBENCH_OUT_NDJSON`.

## Output schema

One `Run` (`emit.go`) per opcode, written as CSV and/or NDJSON:

| Field | Meaning |
|---|---|
| `input_id` | opcode id, e.g. `ADD` |
| `class` | opcode family, e.g. `arithmetic` |
| `reps` | opcode executions the delta represents |
| `gas_used` | whole-program gas delta (target - baseline); per-op = `gas_used/reps` |
| `exec_time_ns` | whole-program time delta, ns; per-op = `exec_time_ns/reps` |
| `status` | `ok` if `significant`, else `insignificant` — never gated on CoV |
| `iterations` | timed iterations behind each series |
| `cov` | worse (max) of the baseline/target series CoV — advisory only |
| `significant` | the acceptance gate |
| `high_variance` | advisory: CoV exceeded `CoVCeiling` |
| `nvcsw` / `nivcsw` | worse (max) of the baseline/target voluntary/involuntary context-switch counts |

A per-`Case` self-check (`bench_test.go`) also verifies the measured gas
delta matches the definitional expected delta from `BuildCaseWith`'s algebra
— a correctness check on the harness construction itself, not on opcode
timing. All 22 `Specs` entries were hand-verified against the fork's
jump-table `minStack`/`maxStack`; verify a future addition the same way.

## Scope

Cleanly-benchmarkable-as-a-scalar opcodes only (arithmetic/bitwise/
comparison/stack/control). Deferred, not yet implemented: parametric-curve
opcodes (`KECCAK256`/`EXP`/copy/memory/`LOG` by size), the state-touching
context-dependent matrix (`SLOAD`/`SSTORE` by warm/cold, `CALL` family,
`CREATE`), real-block replay (F1), and Sei's custom precompiles (0x1001+,
off-model — cosmos-module time, not EVM).
