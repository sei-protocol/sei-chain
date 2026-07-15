# gasbench

Differential microbenchmark harness: per-opcode EVM execution time vs gas
cost, tracer-free. See `README.md` for the full rationale (differential
construction, acceptance-gate design, active-benchmarking diagnostics); this
file is the short orientation.

## Rules for changes here

- **Never attach a tracer to a timed run.** `debug=true` in the interpreter
  dilates per-step time. Whole-program gas needs no tracer, so it's read off
  the same tracer-free call that's timed — there is no separate gas-only
  pass. A future per-opcode gas breakdown *would* need a tracer, and that
  breakdown must come from a separate, untimed call. See README.md.
- **Correlate against EVM gas, never the Cosmos gas meter.** `Program.Run`
  deliberately has no ante handler / `GasMeter` in its path.
- **A new `Case` must terminate cleanly** — every `Case` `BuildCaseWith`
  builds ends in STOP with a balanced, net-zero-gas stack by construction;
  `NewProgram` rejects anything that doesn't run clean once, and
  `bench_test.go`'s self-check will fail loudly if the algebra is wrong. (The
  general `Program.Run` contract also accepts RETURN — see README.md "Error
  contract" — but every `Case` built here uses STOP.)
- **New opcode specs:** hand-verify `Arity` (against the fork's
  `core/vm/jump_table.go` `minStack`/`maxStack`) and `ConstGas` (against
  `core/vm/jump_table.go` + `core/vm/eips.go`); the self-check catches a
  wrong `ConstGas` but not a self-consistent wrong `Arity` — this is the
  authoritative verification checklist, README.md's mention of it points
  back here. `ConstGas` must come from the geth constant, not a Sei
  override — production never reprices a scalar opcode (stock `vm.Config{}`,
  no custom jump table); the only Sei gas param is `SeiSstoreSetGasEIP2200`,
  a storage opcode and out of scope here.
- **Data-dependent ops: verify the operands hit the real kernel.** For a
  `DataDependent` spec, confirm `seedOperands` exercise the intended path, not
  a `holiman/uint256` short-circuit — equal or degenerate operands make
  `DIV(x,x)`/`MOD(x,x)` return without dividing, so the row would time a
  compare. The gas self-check will NOT catch this (gas is unchanged); it's a
  timing trap. `seedOperands` is ascending (dividend above divisor, smallest at
  the modulus slot) for this reason — see README.md "Differential construction".
- Keep code comments lean (what, not why); put methodology/rationale in
  README.md instead of growing doc comments.

## Running

```bash
tools/gasbench/run.sh
```

End-to-end walkthrough incl. how to interpret the output: README.md
"Quickstart". Env vars, output schema, and the `-count` semantics caveat:
README.md "Running it" / "Output schema".

## Files

| File | Contents |
|---|---|
| `gasbench.go` | timing core: `Measure`, `Config`, `Series`, rusage snapshot |
| `diff.go` | `Subtract`: per-pass baseline/target differencing, `Diff` (medians/delta/gas/CoV) |
| `stats.go` | `Summarize`: median/stddev/CoV over a sample series |
| `crossrun.go` | `analyzeCrossRun`: cross-run benchmath verdict (paired delta CI gate + advisory Mann-Whitney p) |
| `program.go` | `Program`: warmed tracer-free EVM environment, one bytecode input |
| `programs.go` | `OpSpec`/`Specs`/`Case`/`BuildCaseWith`: the differential bytecode construction |
| `emit.go` | `Run`, the verdict (`distinguishable`/`classifyStatus`), CSV/NDJSON output |
| `emit_test.go` | pins `Run.Status`/CI as pure functions of the `crossRun` verdict (`classifyStatus`, `distinguishable`), the effect floor, and D-1 null-CI writer safety |
| `crossrun_test.go` | pins `analyzeCrossRun`: drift-survival (paired beats unpaired), straddles-zero, underpowered |
| `diff_test.go` | pins `Subtract`'s per-pass contract (delta/gas/per-op) |
| `programs_test.go` | pins the `BuildCaseWith` arity-0 panic guard |
| `bench_test.go` | `BenchmarkOpcodes`: wires the above into `go test -bench` |
| `run.sh` | pinned-core runner + operator checklist for turbo/governor/isolation |
| `README.md` | operator quickstart + full rationale: construction, acceptance gate, diagnostics |
| `AGENTS.md` | this file |
