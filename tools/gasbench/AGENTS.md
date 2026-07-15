# gasbench

Differential microbenchmark harness: per-opcode EVM execution time vs gas
cost, tracer-free. See `README.md` for the full rationale (differential
construction, acceptance-gate design, active-benchmarking diagnostics); this
file is the short orientation.

## Rules for changes here

- **Never attach a tracer to a timed run.** `debug=true` in the interpreter
  dilates per-step time — gas and time are always measured in separate,
  tracer-free runs. See README.md.
- **Correlate against EVM gas, never the Cosmos gas meter.** `Program.Run`
  deliberately has no ante handler / `GasMeter` in its path.
- **A new `Case` must terminate cleanly** (STOP, balanced stack, net-zero
  gas per unit) — `NewProgram` rejects anything that doesn't run clean once,
  and `bench_test.go`'s self-check will fail loudly if the algebra is wrong.
- **New opcode specs:** hand-verify `Arity`/`ConstGas` against the fork's
  `core/vm/jump_table.go` + `core/vm/eips.go`; the self-check catches a wrong
  `ConstGas` but not a self-consistent wrong `Arity`. `ConstGas` must come
  from the geth constant, not a Sei override — production never reprices a
  scalar opcode (stock `vm.Config{}`, no custom jump table); the only Sei gas
  param is `SeiSstoreSetGasEIP2200`, a storage opcode and out of scope here.
- Keep code comments lean (what, not why); put methodology/rationale in
  README.md instead of growing doc comments.

## Running

```bash
tools/gasbench/run.sh
```

Env vars, output schema, and the `-count` semantics caveat: see README.md
"Running it" / "Output schema".

## Files

| File | Contents |
|---|---|
| `gasbench.go` | timing core: `Measure`, `Config`, `Series`, rusage snapshot |
| `diff.go` | `Subtract`: baseline/target differencing, `Diff`, the acceptance gate |
| `stats.go` | `Summarize`: median/stddev/CoV/standard-error over a sample series |
| `program.go` | `Program`: warmed tracer-free EVM environment, one bytecode input |
| `programs.go` | `OpSpec`/`Specs`/`Case`/`BuildCaseWith`: the differential bytecode construction |
| `emit.go` | `Run`, CSV/NDJSON output |
| `bench_test.go` | `BenchmarkOpcodes`: wires the above into `go test -bench` |
| `run.sh` | pinned-core runner + operator checklist for turbo/governor/isolation |
