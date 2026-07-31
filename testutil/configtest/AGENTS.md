# Configuration Characterization

`testutil/configtest` and `testutil/fuzzing` are the harness that pins how a seid
node resolves configuration. The suites built on them record the current behavior
of the legacy configuration path as executable tests, including the parts of that
behavior nobody would choose on purpose.

The surface is worth pinning because it is large and mostly implicit. A value
reaches running code from four layers (an in-code default, a TOML file, an
environment variable, a cobra flag), resolved through several viper instances whose
environment prefixes differ, and it lands in two places the rest of the boot reads,
a Tendermint config struct and a flat key-value map. None of that was written down
anywhere a second implementation could be compared against, which is what these
tests supply. They exist so the SeiConfigManager work (PLT-775) can replace that
path and prove the replacement resolves every key the same way.

## Standing Rule

A change to how a configuration value is read, defaulted, named, or cast is a
change to a pinned contract, so the suite fails. That failure is the review prompt.
Record the new behavior and put the old and new value in the diff, rather than
loosening the assertion until it passes.

Three ways of making a failure go away are wrong here, because each one turns a
visible change into an invisible one:

1. `t.Skip` on a row whose behavior changed. CI stays green and a skip line in
   verbose output is what nobody reads.
2. Widening an assertion to accept both the old and new value.
3. Deleting a row rather than updating it.
4. Editing a row's `Cast`, `Unguarded` or `Checked` until it matches a reader you
   changed, without having intended the behavior change. A row describes the reader,
   so editing one is correct only alongside a deliberate change to that reader in the
   same PR.

If a pinned behavior is genuinely wrong and worth fixing, fix it in the production
reader and update the row in the same PR. The row then records the improvement.

## Adding a Key to an Existing Section

A section's manifest is a `[]configtest.KeySpec`, one row per key its reader looks
up. Add a row describing the read as written:

```go
{
    Key: "evm.max_log_bytes", Path: "MaxLogBytes", Cast: configtest.CastInt64, Checked: true,
    Why: "bounds the response bytes an eth_getLogs may return, so it caps peak memory per query",
},
```

`Cast`, `Unguarded` and `Checked` describe the reader as written, not the behavior
you would prefer it had. `Checked: true` says the read uses a `cast.ToXE` form and
propagates the conversion failure. `Unguarded: true` says the read has no
`v != nil` check, so an absent key overwrites the in-code default with a zero, and
both omitted fields default to false. Those are real properties of the legacy path,
and recording them is the difference between pinned and accidentally still true.

Then seed the row so an ordinary `go test` run reaches it rather than only the
fuzzer. Seeds name their shape through the `fuzzing.Kind*` constants:

```go
f.Add(uint(idx), fuzzing.KindNil, "", int64(0), false)          // absent-key path
f.Add(uint(idx), fuzzing.KindString, "not-a-number", int64(0), false) // malformed input
```

Write one row per key, including when two keys land in the same struct field. The
manifest is what the differential enumerates, so a key with no row is a key the
comparison never makes.

`CheckManifestCoversEveryField` covers the weaker half of that automatically: every
resolved field must be named by some row's `Path` or `AlsoWrites`, or exempted at the
call site. It works on fields rather than keys, so it catches a field no row claims
and cannot catch a second key landing in a field some other row already claims. That
case is the one the per-key rule above exists for, and nothing mechanical will
prompt you.

## Changing a Default

Section defaults are recorded in `testdata/<section>.golden` and compared on every
run. Regenerate the file and review the diff:

```bash
go test ./evmrpc/config/ -run TestDefaultsMatchTheRecordedValues -update
```

Pass `-update` per package rather than tree-wide. The flag is only registered in
binaries that link the harness, so `go test ./... -update` makes every other package
exit with "flag provided but not defined".

The recorded file is the anchor a self-comparison cannot provide. A test that reads
a section with no keys set and compares the result against that same package's
default struct passes for whatever the value happens to be, because both sides move
together. The golden file makes a changed default show up as a diff a reviewer sees.

That only holds if the diff is read. An `-update` commit keeps the golden change on
its own and names the old and new value in the PR body, so a changed default is
reviewed rather than carried along inside a regenerated file. Running `-update` to
clear a diff you cannot account for defeats the mechanism, since the unexplained
value is exactly what the file exists to surface.

A default computed from the machine cannot sit in a golden file as a literal, since
`runtime.NumCPU()` records a value true only of the machine that generated it.
Declare it as a `DerivedDefault` instead, which masks the field in the file and
asserts the formula in its place:

```go
configtest.CheckDefaults(t, "evm", config.DefaultConfig,
    configtest.DerivedDefault{
        Path: "WorkerPoolSize",
        Want: min(config.MaxWorkerPoolSize, runtime.NumCPU()*2),
        Why:  "min(MaxWorkerPoolSize, runtime.NumCPU()*2)",
    })
```

## Adding a Section

A new section needs four things, and the fourth is the one that is easy to miss:

1. `CheckDefaults` against a checked-in `testdata/<section>.golden`.
2. `CheckAbsent`, asserting that a reader handed no keys returns exactly the
   package's declared defaults.
3. A `[]KeySpec` manifest with a `CheckRow` fuzz target, seeded per row.
4. `CheckManifestCoversEveryField`, so the manifest cannot silently fall behind the
   reader.

## Running

```bash
go test ./testutil/... ./app/ ./evmrpc/config/          # the suites, ordinary run
go test -race ./<pkg>/                                  # the bar CI holds
go test ./<pkg>/ -run FuzzXxx -fuzz FuzzXxx -fuzztime 60s   # extend a target by hand
```

Seeds cover every row on an ordinary run. Running the fuzzer by hand explores past
the seeds and is worth doing when changing a reader's cast or its guard.

## Hermeticity

These tests control more than the arguments to the function under test, because the
process environment, `$HOME`, and the executable basename all feed the result.

- `configtest.Isolate` pins the environment to a known-empty state. Use it in any
  test that reads configuration, or a stray variable in the developer's shell
  changes the outcome.
- The server viper's environment prefix is `path.Base(os.Executable())`, so the
  test binary's name is part of the input. `ServerEnvPrefix` reports it rather than
  assuming `seid`.
- Never call `t.Parallel` in a test that touches the environment or the working
  directory. Both are process-global. Use `t.Chdir`, which restores on cleanup and
  enforces that rule.
- `configtest.NewHome` builds a fixture node directory. The legacy path treats a
  node directory as read-write during a read, creating an absent `config.toml` with
  hardcoded overrides and materializing `app.toml` from whatever the viper holds, so
  a test that reuses a real home is not reproducible.
- A fuzzer-generated string is not always writable as TOML. `IsTOMLWritable` and
  `EnvValueIsSettable` decline the values with no faithful spelling, which keeps a
  parse failure from being attributed to the layer under test.

## Out of Scope

The suite covers the viper resolution and the keys `app.New` reads back out of the
flat map. Two classes sit outside it:

- Reads that need a running node, tracked in PLT-851.
- Direct environment and file reads that bypass the boot path entirely, such as
  `UPGRADE_VERSION_LIST` in `app/upgrades.go`. These carry their own tests and
  migrate onto the configuration SDK per call site.
- A file-layer comparison. The rows drive a reader at the `AppOpts` map level, which
  is the layer the new manager has to reproduce, so nothing here asserts that a value
  written as TOML resolves the same way as the same value handed over as a map. That
  axis would need a value-to-TOML renderer, and it is unbuilt.
