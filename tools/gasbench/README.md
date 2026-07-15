# gasbench

Differential microbenchmark harness that measures per-opcode EVM execution
time and correlates it with gas cost. Built to unblock the gas-repricing
project's F2 hypothesis test: does gas cost track execution time — the same
"gas is not a faithful meter of execution cost" question Broken Metre (Perez &
Livshits, NDSS 2020, arXiv 1909.07220) posed for mainnet, answered here
fork-natively. The method — isolate an opcode by repetition, then convert
runtime to gas — is the accepted state of the art (EIP-7904, Compute Gas Cost
Increase), whose anchor-rate / MGas·s⁻¹ vocabulary the repricing framing adopts.

Design: `designs/gas-repricing-telemetry/gas-vs-time-instrumentation.md`
(harness) and `gasbench-benchmath-integration.md` (the cross-run statistics and
acceptance gate) in the `bdchatham-designs` repo; prior-art lineage in
`research/gasbench-prior-art-scan.md`. Noise-floor calibration behind the
acceptance gate below:
`designs/gas-repricing-telemetry/research/microbenchmark-noise-isolation-tradeoffs.md`
in the same repo.

## Quickstart

To iterate on this harness with an agent, point it at this directory's
`AGENTS.md` first — it carries the rules a change here must not break.

**1. Smoke run (any machine, indicative numbers only):**

```bash
cd sei-chain
GASBENCH_OUT_CSV=gasbench.csv GASBENCH_OUT_NDJSON=gasbench.ndjson \
  tools/gasbench/run.sh | tee gasbench.log
```

This benchmarks every scalar opcode in `Specs` and writes ONE aggregate row
per opcode to the output files (`-count=10` by default: `-count` reruns each
subtest inside one benchmark invocation, and benchmath folds those passes into
the row's cross-run CI — the file is written once at the end).
Relative output paths land in `tools/gasbench/` (the test binary's working
directory), not where you invoked `run.sh` — pass absolute paths to put
them elsewhere. The `tee` is optional: it captures the `go test` log (per-opcode
metrics, host-health warnings), not measurement data — the CSV/NDJSON aggregate
is the output of record. Roughly ten minutes at the defaults; set
`GASBENCH_ITERS=2000 GASBENCH_COUNT=1` for a fast first look (note `-count=1` is
`underpowered` by construction — no finite CI). On a laptop or shared box,
`run.sh` will warn that results are indicative: expect cheap opcodes (`ADD`'s
per-op cost is ~1 ns, a whole-program delta close to the noise) to come back
`insignificant` or `sub_threshold` there, and don't read anything into them.

**2. For real numbers, use a quiet, dedicated Linux host:** bare metal (no
co-tenants), fixed or pinned CPU frequency, one core reserved for the run
(`GASBENCH_CORE`). `run.sh`'s header comment is the operator checklist. A
fixed-frequency ARM server instance (e.g. a Graviton `.metal`) needs the
least setup because there is no turbo/governor to disable. Kernel-level
isolation (`isolcpus` etc.) is deliberately NOT required — see "Acceptance
gate" below for why plain pinning is enough.

<details>
<summary><b>EC2 recipe (the exact workflow behind the shipped numbers)</b></summary>

A `c6g.metal` (Graviton2: fixed frequency, no SMT, no co-tenants) is the
least-setup host that satisfies the checklist. Ephemeral workflow via SSM —
no SSH keys, no inbound ports:

```bash
# 1. Launch. Needs: an AL2023 arm64 AMI, any egress-capable subnet/SG, and an
#    instance profile carrying AmazonSSMManagedInstanceCore.
AMI=$(aws ssm get-parameter \
  --name /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64 \
  --query 'Parameter.Value' --output text)
IID=$(aws ec2 run-instances --image-id "$AMI" --instance-type c6g.metal \
  --subnet-id <subnet> --security-group-ids <sg> \
  --iam-instance-profile Name=<ssm-instance-profile> \
  --block-device-mappings 'DeviceName=/dev/xvda,Ebs={VolumeSize=60,VolumeType=gp3}' \
  --query 'Instances[0].InstanceId' --output text)

# 2. Wait for SSM Online (bare metal boots in ~10 min), then bootstrap + run.
#    Building sei-chain's module graph needs the 60GB root and a few minutes.
aws ssm send-command --instance-ids "$IID" --document-name AWS-RunShellScript \
  --parameters '{"commands":[
    "dnf install -y -q git tar gzip",
    "curl -sL https://go.dev/dl/go1.25.6.linux-arm64.tar.gz | tar -C /usr/local -xz",
    "export HOME=/root PATH=/usr/local/go/bin:$PATH",
    "git clone --depth 1 https://github.com/sei-protocol/sei-chain.git /opt/sei-chain",
    "cd /opt/sei-chain && go test -count=1 ./tools/gasbench/...",
    "mkdir -p /opt/out && cd /opt/sei-chain && GASBENCH_OUT_CSV=/opt/out/gasbench.csv GASBENCH_OUT_NDJSON=/opt/out/gasbench.ndjson tools/gasbench/run.sh > /opt/out/run.log 2>&1"
  ],"executionTimeout":["3600"]}'

# 3. Fetch results (SSM output is text-only; base64 the CSV through it),
#    then TERMINATE — the box bills ~$2.2/hr and the run needs ~15 min total.
#    (send "base64 /opt/out/gasbench.csv", decode locally)
aws ec2 terminate-instances --instance-ids "$IID"
```

The full suite at defaults is ~11 min on this host; expect every row
`status=ok` with CI half-widths well under 1% of `exec_time_ns`. `run.sh`
prints `cannot verify turbo/governor` on Graviton — expected: there is no
turbo to check, which is the point of the host choice.

</details>

**3. Read the output:** each row is one opcode's *differential* cost — the
target-minus-baseline delta, so setup/loop overhead has already cancelled
(see "Differential construction"):

- **per-op time** = `exec_time_ns / reps`.
- **two gas columns, and they are not the same number.** `const_gas` is the
  nominal gas the chain actually charges for the opcode (the jump-table
  constant: ADD=3, MULMOD=8). `gas_used` is the *differential* whole-program
  delta the harness measured — net of the filler the baseline leaves behind —
  so it is smaller (ADD→1, MULMOD→4) and arity-dependent. `gas_used` exists to
  drive the construction self-check; **`const_gas` is the denominator for any
  repricing statement.**
- **ns-per-gas** = `exec_time_ns / reps / const_gas` — **divide by `const_gas`
  (nominal), never `gas_used`**: the differential denominator is deflated by an
  arity-correlated factor (up to 3x) and would report a spread even for a
  perfectly-priced instruction set. This is the number the hypothesis test is
  about: time per gas-the-chain-charges (EIP-7904 frames the same quantity as an
  anchor rate — its 100 MGas/s ≈ 10 ns/gas). If pricing tracked execution time
  it would be roughly constant across opcodes; the spread IS the mispricing
  signal. Quote the spread
  only across `status==ok` rows: the cheap ops (DUP1, ADD…) are dominated by the
  op-minus-filler marginal and routinely come back `insignificant` or
  `sub_threshold` on a normal host — do not anchor the low end of the spread on
  one. Comparing rows within
  one run cancels the common clock-speed factor, so the spread is meaningful
  even though the absolute nanoseconds are host-specific — but microarchitectural
  ratios (adder vs shifter vs multiplier cost) still differ between hosts, which
  is why repricing-grade numbers come from the step-2 host, not a laptop.

**4. Only trust a row that earns it — filter on `status`:**

- `status=ok` is the only correlation-eligible verdict: the paired delta CI
  excludes zero (distinguishable) AND the per-op median delta clears the
  effect-size floor (`GASBENCH_MIN_PEROP_NS`, default 1.0 ns). Sign-check
  `exec_time_ns`: a negative value is a measured *speedup* (target faster than
  baseline), still distinguishable — the gate is "measurably different," never
  "more expensive."
- `status=sub_threshold`: distinguishable but below the effect floor — "not
  measurably more expensive in absolute time at this precision." This is NOT
  "correctly priced": a cheap op can still be mispriced. Do not feed
  `sub_threshold` rows into the ns-per-gas correlation.
- `status=insignificant`: the delta CI straddles zero — indistinguishable from
  no difference at this precision. Raise `GASBENCH_ITERS` or `-count`, or move
  to the quiet host; don't average or rank insignificant rows.
- `status=underpowered`: too few counts for a finite CI (`-count<6` at the
  default confidence — precisely: `<2` counts, or any count whose CI bound is
  non-finite at the requested confidence; see "Output schema"); `ci_lo`/`ci_hi`
  are null. Raise `GASBENCH_COUNT`.
- `status=error`: the case never produced a measurement — its subtest failed
  (invalid program, `Measure` error) before accumulating a count. Numerics are
  zero, CI null; see the test log for the underlying failure.
- `high_variance=true`: advisory — the run saw unusual dispersion (noisy
  neighbor, throttling). It does not invalidate an `ok` result, and
  `nivcsw` tells you where to look: nonzero means the scheduler preempted
  the process mid-window (blame the host); near-zero alongside the high
  CoV points at a non-scheduler tail inside the harness itself (see
  "Active-benchmarking diagnostics").
- The cross-count agreement check is now IN the row, not something you
  reconstruct: `run.sh` defaults to `-count=10`, and benchmath folds those 10
  passes into the paired delta CI (`ci_lo`/`ci_hi`, at `confidence`). A tight CI
  that excludes zero IS the stable-across-counts result; a wide one that
  straddles zero is the noise verdict. There is no longer a per-count row to
  eyeball or a `benchstat`-consumable stdout to pipe — the aggregate row is the
  output of record. On a dedicated pinned host, expect the CI half-width to be a
  low-single-digit percent of `exec_time_ns`. See "Running it" for what
  `-count` does and doesn't give you.

**Scope:** scalar constant-gas opcodes only (arithmetic/bitwise/comparison/
stack). Parametric opcodes (`KECCAK256`/`EXP`/copies), the state-touching
matrix (`SLOAD`/`SSTORE`/`CALL`), and real-block replay are deferred — see
"Scope" at the bottom for the un-defer triggers. To add an opcode within
scope, follow `AGENTS.md` "New opcode specs".

## The load-bearing invariant: never attach a tracer to a timed call

Attaching a `tracing.Hooks` tracer to sum per-opcode gas sets `debug=true` in
the interpreter loop, which dilates per-step execution time. This harness
only measures whole-program gas (not a per-opcode breakdown), and
whole-program gas needs no tracer at all: `Program.Run`
(`GasLimit - leftOverGas` via `runtime.Call`, not `runtime.Execute`, which
discards `leftOverGas`) returns it directly. So the *same* tracer-free call
that's timed also yields its gas — `Measure` (`gasbench.go`) reads `gasUsed`
from the identical `run()` invocation that `time.Since` wraps. There is no
separate gas-only pass in this harness.

**If a future phase adds a per-opcode gas breakdown**, that requires
attaching a tracer, and the timed run must stay tracer-free — that
breakdown would have to come from a *separate* tracer-on call whose timing
is never read, not from the timed call.

## Differential construction

Each `Case` (`programs.go`) is a baseline/target bytecode pair, identical
except the target executes the opcode under test. Everything the two
programs share — loop overhead, setup, dispatch cost — cancels when
`Subtract` (`diff.go`) takes the difference; what remains is attributable to
the opcode.

The repeating unit is balanced so gas is net-zero and the stack never grows:
for an opcode consuming `n` operands and producing 1 result, `n` *distinct*
operands sit at the base of the stack and each unit lifts fresh copies of all
`n` with `DUP<n>` (`DUP<n>` copies the deepest of the `n`; repeated `n` times
it re-lifts the whole tuple in order):

```
setup    = PUSH v0 .. PUSH v_{n-1}        // n distinct operands, once
target   = DUP<n> x n , OP     , POP      // lift the n-tuple, run op, drop result
baseline = DUP<n> x n , POP x n           // lift the n-tuple, drop them
```

The `n` DUP<n>s cancel, one POP cancels the target's trailing POP, so the
per-unit gas delta is `ConstGas(op) - (n-1)*GasQuickStep` (every `DUP<k>` is
`GasFastestStep`, so the fill choice doesn't change the algebra).

**The operands must be distinct, and this is load-bearing, not cosmetic.**
`holiman/uint256` short-circuits degenerate operands before the real kernel:
`DIV(x,x)→1` and `MOD(x,x)→0` return without a division, so feeding `n` copies
of one value (an earlier version's `DUP1 x n`) would time an `Eq` compare, not
a 256-bit divide — understating DIV/MOD by ~10x and inverting their place in
the spread. `seedOperands` is ascending so the arity-2 dividend (top) stays
above the divisor and DIV/MOD reach `udivrem`, and the smallest value lands at
the modulus slot so ADDMOD/MULMOD operands aren't pre-reduced. Two opcode
families are still special-cased in `BuildCaseWith`:

- **Stack ops** (`DUP1`, `SWAP1`) aren't "n operands → 1 result" — each gets
  its own construction.
- **SHL/SHR/SAR** need a small *in-range* shift amount on top (not a full-width
  value): an out-of-range shift takes go-ethereum's constant-time early-out
  (the cheapest case; `value.Clear()`, or `SetAllOne()` for SAR on a negative
  operand) — at shift ≥256 for SHL/SHR, shift >256 for SAR — so a full-width
  top operand would measure that early-out instead of the real limb-shift path.
  `seedShift` keeps the shift amount in-range and distinct from the value.

`EXP` and anything with dynamic/memory/state gas is out of scope — see
`OpSpec.DataDependent` and the design's Non-goals.

## Gas isolation from the Cosmos layer

`Program.Run` builds a bare go-ethereum EVM (`runtime.Call`) with no ante
handler and no Sei `GasMeter`. `gasUsed` is pure EVM gas
(`cfg.GasLimit - leftOverGas`); there is deliberately no Cosmos gas meter in
this path. Correlate against **EVM** gas, not the Cosmos gas meter — mixing
the two units was the CON-368 trap (`x/evm/keeper/msg_server.go`'s
`serverRes.GasUsed` assignment, which is in EVM's gas unit, not Sei's).

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

This periodic reallocation is also the one noise source that inflates the
coefficient of variation (CoV, stddev/mean — see "Acceptance gate" below)
without the `getrusage`-based diagnostics ("Active-benchmarking diagnostics"
further below) being able to see it (it's not a scheduler preemption).

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

## Acceptance gate: paired delta CI + effect floor, not CoV

The verdict lives in `emit.go` (`distinguishable`/`effectSizePass`/
`classifyStatus`), computed at the cross-run (`-count`) layer by
`benchmath.AssumeNothing` (`crossrun.go`). It has two independent halves; a row
is accepted (`status=ok`) only if BOTH clear.

**1. Distinguishable — the statistical half.** Each `-count` pass measures
baseline then target back-to-back in one pinned, GC-off window, so the design
is *paired*: `delta_k = target_median_k − baseline_median_k`. The gate is the
order-statistic CI on that per-count delta series excluding zero —
`distinguishable := ci_lo > 0 || ci_hi < 0` (`benchmath.AssumeNothing.Summary`
over the deltas). It is symmetric: a CI wholly below zero (a measured speedup)
is just as distinguishable as one wholly above — the gate is "measurably
different," never "more expensive."

Paired, not unpaired, is load-bearing. Across-count common-mode drift
(frequency wander, cache-state shift — exactly what `-count` exists to survive)
moves both series together, so the paired delta cancels it. An unpaired
two-sample test over the baseline-median series vs the target-median series
loses the signal under that drift: a true +3 ns/op with ±30 ns shared drift
makes the two median series overlap, while the paired deltas `[3,3,3,…]` are
unmistakable and their CI cleanly excludes zero. `benchmath` exposes no
paired/signed-rank test, so the CI on the delta series IS the paired primitive.
The unpaired Mann-Whitney U p (`p_value`, over the baseline vs target median
series) is emitted as an **advisory column only** for cross-checking — NOT the
gate, so its known weaknesses (tie fragility at integer-ns medians, power loss
under shared drift) never touch the acceptance decision.

**2. Effect-size floor — the practical half.** Distinguishable at large n is
not the same as meaningful. `status=ok` additionally requires the per-op median
delta (`exec_time_ns / reps`) to clear `GASBENCH_MIN_PEROP_NS` (default 1.0 ns,
compared in absolute value so a speedup still counts). A row that is
distinguishable but below the floor is `sub_threshold`, NOT `ok`.

`sub_threshold` means "not measurably more expensive in absolute time at this
precision" — it does NOT mean "correctly priced." A cheap-but-mispriced op can
be demoted on the absolute-ns floor even though its ns-per-gas is an outlier;
the floor is a *measurability* guard, not a pricing verdict. **Only `status=ok`
rows are eligible for the ns-per-gas repricing correlation.** The 1.0 ns default
is a consumer-tunable calibration (it may demote ADD/DUP-class boundary ops on
some hosts — cite the noise-isolation research when retuning); set
`GASBENCH_MIN_PEROP_NS<=0` to disable the floor entirely. A gas-relative floor
(ns-per-nominal-gas deviation) is deferred pending the reference-anchor choice
(EIP-7904's 100 MGas/s is the citable candidate but is host-frequency-specific).

(Distinguishability is decided at the cross-run n=10 layer, not the within-run
n≈20000 layer — at within-run n the test is over-powered, any sub-nanosecond
shift clears it; the effect floor is the second half of the gate.)

**CoV is advisory, never the gate.** A raw per-series CoV of several percent
under plain core-pinning (no kernel-level isolcpus/nohz_full/rcu_nocbs) is
expected physics (the periodic scheduler tick, device IRQs, shared-cache
contention that pinning alone can't remove), not a defect. No major
benchmarking harness (JMH, criterion.rs, Go's own `benchstat`) gates on a
fixed dispersion threshold either — all three gate on effect size against
the estimate's own uncertainty.

`Diff.HighVariance` (CoV above `CoVCeiling`, default 0.25) is advisory only:
it flags a run worth a closer look (a noisy neighbor, a throttling event) but
never overrides `status`. The 0.25 default sits above the 4-8% CoV
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
`Measure`'s, not `b.N`'s, so `b.N` must stay at 1. `-count=K` reruns each
opcode subtest K times **within the same OS process** (inside a single
`BenchmarkOpcodes` invocation); benchmath folds those K passes into the one
aggregate row's cross-run CI. This is cross-run variance, not process-level
independence — true process-level independence would need K separate `go test`
invocations. See the research doc for why this matters less than it sounds:
JMH's fork/warmup/measurement-iteration model is the gold standard, but this
MVP's within-process reruns already surfaced reproducible per-opcode deltas
(see the research doc's empirical run). **`-count>=6` is needed for a finite CI**
at the default confidence; below that a row reads `underpowered` with a
benchmath warning, so the default `-count=10` has headroom.

Env vars:

- `GASBENCH_WARMUP`, `GASBENCH_ITERS` — override `DefaultConfig`'s within-run
  warmup / timed iterations.
- `GASBENCH_COUNT` (default 10) — cross-run `-count` passes; see the finite-CI
  minimum above.
- `GASBENCH_ALPHA` (default 0.05) — `CompareAlpha` behind the advisory
  `p_value` (Mann-Whitney U); does not affect the gate.
- `GASBENCH_CI_CONFIDENCE` (default 0.95) — confidence for the paired delta CI
  (the gate). Raising it raises the minimum `-count` for a finite CI (~6 at
  0.95), so bump `GASBENCH_COUNT` in lockstep.
- `GASBENCH_MIN_PEROP_NS` (default 1.0) — effect-size floor on `|per-op median
  delta ns|`; distinguishable-but-below → `sub_threshold`. Set `<=0` to disable.
- `GASBENCH_COV_CEILING` (default 0.25) — advisory `high_variance` ceiling on
  per-series CoV; never gates the verdict.
- `GASBENCH_OUT_CSV`, `GASBENCH_OUT_NDJSON` — output paths.

## Output schema

One `Run` (`emit.go`) per opcode, written as CSV and/or NDJSON. Columns are in
`csvHeader` order:

| Column | Meaning |
|---|---|
| `input_id` | opcode id, e.g. `ADD` |
| `class` | opcode family, e.g. `arithmetic` |
| `reps` | opcode executions the delta represents |
| `gas_used` | *differential* whole-program gas delta (target − baseline), net of filler; per-op = `gas_used/reps`. Drives the construction self-check — NOT what the chain charges, NOT the repricing denominator |
| `const_gas` | nominal per-op gas the chain charges (jump-table `ConstGas`); the denominator for ns-per-gas — see "Read the output" |
| `exec_time_ns` | **median** whole-program time delta over the `-count` passes (`Summary.Center`), ns; always finite; per-op = `exec_time_ns/reps`. **Sign-check it: a negative value is a measured speedup.** |
| `count` | cross-run n: number of `-count` passes aggregated |
| `iterations` | within-run timed iterations behind each series |
| `ci_lo` | order-statistic CI low on `exec_time_ns` (whole-program ns). **Nullable**: empty CSV field / JSON `null` when the bound is non-finite (underpowered) |
| `ci_hi` | CI high; same nullable rule |
| `confidence` | actual CI confidence (≥ requested) |
| `distinguishable` | **the gate, statistical half:** paired delta CI excludes zero (`ci_lo>0 \|\| ci_hi<0`), computed from the raw benchmath bounds — on an underpowered row the emitted `ci_lo`/`ci_hi` are null and this reads `false` |
| `effect_size_pass` | the gate, practical half: per-op median delta clears `GASBENCH_MIN_PEROP_NS` |
| `p_value` | **advisory only:** unpaired Mann-Whitney U p over baseline vs target medians; NOT the gate |
| `alpha` | `CompareAlpha` behind the advisory `p_value` |
| `status` | `ok \| sub_threshold \| insignificant \| underpowered \| error` — see below; never gated on CoV |
| `cov` | worse (max) of the baseline/target series CoV, max across counts — advisory only. CSV rounds to 6 sig figs for readability; NDJSON carries full precision — expect tail digits to differ across formats |
| `high_variance` | advisory: CoV exceeded `CoVCeiling` on any count |
| `nvcsw` / `nivcsw` | worse (max) of the baseline/target voluntary/involuntary context-switch counts, max across counts |

`status` enum:

- `ok` — distinguishable AND clears the effect floor. **The only
  correlation-eligible verdict.**
- `sub_threshold` — distinguishable but below the effect floor: "not measurably
  more expensive in absolute time," NOT "correctly priced" (a cheap op can
  still be mispriced).
- `insignificant` — the delta CI straddles zero: not distinguishable at this
  precision.
- `underpowered` — too few counts for a finite CI (`count<2`, or a non-finite
  bound at the requested confidence); `ci_lo`/`ci_hi` are null.
- `error` — reserved for a per-case measurement failure; not currently emitted
  (an invalid program fails the benchmark loudly instead).

**Consumer note:** filter on `status` (only `ok` is correlation-eligible) and
sign-check `exec_time_ns` (a negative value = a measured speedup). `ci_lo`/`ci_hi`
are null on underpowered rows — an older CSV wrote `+Inf` here, the current CSV
writes an empty field.

A per-`Case` self-check (`bench_test.go`) also verifies the measured gas
delta matches the definitional expected delta from `BuildCaseWith`'s algebra
— a correctness check on the harness construction itself, not on opcode
timing. It catches a wrong `ConstGas` but not a self-consistent wrong
`Arity`; see `AGENTS.md` "New opcode specs" for the hand-verification
checklist a new `Specs` entry needs.

## Scope

Cleanly-benchmarkable-as-a-scalar opcodes only (arithmetic/bitwise/
comparison/stack — the four `Class` families in `Specs`). Deferred, not yet
implemented: parametric-curve
opcodes (`KECCAK256`/`EXP`/copy/memory/`LOG` by size), the state-touching
context-dependent matrix (`SLOAD`/`SSTORE` by warm/cold, `CALL` family,
`CREATE`), real-block replay (F1), and Sei's custom precompiles (0x1001+,
off-model — cosmos-module time, not EVM).
