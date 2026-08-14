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

Inside the surface the suite covers, a change to how a configuration value is read,
defaulted, named, or cast is a change to a pinned contract, so the suite fails. That
failure is the review prompt. Record the new behavior and put the old and new value in
the diff, rather than loosening the assertion until it passes.

What that surface is has to be read alongside the rule, because the rule is
unconditional only inside it. A key added to a struct field some row already claims is
not caught, and nothing mechanical will prompt you (`Adding a Key to an Existing
Section`). A rename fails here and still has to be carried by hand into the app.toml
template, the flag registration and the documentation (`Renaming a Key`). And whole
classes of read sit outside the suite (`Out of Scope`). None of that softens the
paragraph above for the reads the suite does cover: there, the failure is not optional
and not something to route around.

Four ways of making a failure go away are wrong here, because each one turns a
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

### The `[experimental]` carve-out

Keys under the `[experimental]` table are **outside** this surface, deliberately and for the same
reason they sit outside the schema fingerprint: a key that may change shape in a patch release
cannot also be a recorded contract. A declaration there owes no `KeySpec` row, no seed, and no
key-names record, and adding one does not fail the suite.

What replaces those is `CheckExperimentalDeclarations` plus `CheckExperimentalGolden`, both called
from `cmd/seid/cmd`. They are a weaker promise on purpose. The registry record makes a change
visible; it does not freeze it.

Promotion is what brings a key inside this surface. The `KeySpec` row, the seed and the key-names
record land in the same change as the promotion, and from then on the standing rule above applies
to it without exception.

## Primitives

`CheckRow` is `CheckKey` plus `CheckDeterministic`, so there is one fewer property than there
are calls. A fuzz target names only `CheckRow` and gets both. The table below is the enumeration,
and `TestGuideListsEveryPrimitive` holds it to the exported surface.

| Check | The failure it prevents | Held against |
|---|---|---|
| `CheckDefaults` | a declared default moves with nothing independent to compare against | `testdata/<section>.golden` |
| `CheckKeyNames` | an operator-facing key is renamed while the row and the reader move together | `testdata/<section>.keys.golden` |
| `CheckKey` | a reader does not resolve `Key` into `Path` through `Cast` | the reader's own empty-`AppOpts` result, with the row's leaf spliced in |
| `CheckDeterministic` | a reader is not a pure function of its `AppOpts` | a second read of the same input |
| `CheckAbsent` | an omitted key resolves to something other than the declared default | the declared defaults struct |
| `CheckManifestCoversEveryField` | a resolved field no row claims | the manifest's `Path` and `AlsoWrites` entries |
| `CheckEveryRowHasADiscriminatingSeed` | a row whose every seed would also pass against a reader that never looks its key up | the recorded seed corpus |
| `CheckSchemaMatchesTheReader` | a section whose keys are declared by a purpose-written struct pairs a key with the wrong setting, or resolves a baseline the reader does not | the reader itself, by writing a probe value under each key and observing which setting changed |
| `CheckAbsentReadDivergences` | a key whose value changes for a node that has it missing, because its reader resolves an absent key to zero rather than to the default beside it | `testdata/<section>.absent.golden`, one row per key with both values |
| `CheckDeclaredSurface` | a key added, removed, renamed or retyped, or a baseline changed, in any declared section | `testdata/<name>.surface.golden`, every section, key and per-mode baseline as text |
| `CheckZeroWhenAbsentMatchesTheReader` | a migration writing a key's default where the node runs its zero, or the reverse | the reader itself, by writing each candidate and requiring the reader's output to be unchanged |
| `CheckWiring` | one of the calls above is deleted | `testdata/wiring_coverage.txt` |
| `CheckExperimentalDeclarations` | a declaration whose name or metadata is refused reaches a binary, where it is inert and every read of it silently returns the default | the registry, and each declaration's own `Check` run against its own default |
| `CheckExperimentalGolden` | a key is added, removed, renamed, re-typed, re-owned or re-defaulted without the change being visible | `testdata/<name>.experimental.golden`, keyed by name |
| `CheckNoExperimentalKeyShadowsThisSection` | an experimental key declares a path a section already owns, so promoting it would put two declarations on one key | that section's own `[]KeySpec`, which only its test binary can see |
| `CheckObservedKeys` | a reader is added or removed without the change to what a node reads being visible, which no amount of reading the tree can find because a key is a string built at its call site | `testdata/<name>.observed.golden`, recorded by wrapping the source a run is given |

The third column is the spec, and it is the one to read before wiring anything. Three of these
compare against a checked-in file, one against the declared defaults, one against the reader's own
output, one against a second read of the same input, one against the manifest, and one against the
seeds the target declared. A check whose right-hand side comes from the same place as its left-hand
side holds for any reader.

Two of them read no prediction column, and that is the invariant any new check
inherits. `CheckKeyNames` is blind to `Path`, `Cast`, `Unguarded` and `Checked`, and
`CheckEveryRowHasADiscriminatingSeed` to `Cast`, `Unguarded` and `Checked`. A check that
read the column it exists to hold could be silenced by editing that column, which is
forbidden move 4 above.

`CheckKey` compares against the reader's own empty-`AppOpts` result rather than the
declared defaults, because some readers fill fields from outside the config.
`CheckAbsent` is what ties that result to the declared defaults, so a section wired for
rows and not for `CheckAbsent` has an unanchored baseline.

`CheckSchemaMatchesTheReader` covers a case the others cannot. A section normally declares
its keys from the type its reader fills, so the tags and the reader move together. Some
sections cannot: the type carries no `mapstructure` tags at all, or tags naming something
other than the keys the reader looks up, and it may live in a tree this repository does not
change. Those sections declare a struct written only to hold the spelling, and nothing
decodes into it.

What then needs holding is which setting each key reaches, and stating that twice proves
nothing, since both statements come from one reading. So this check asks the reader: it
writes a probe value under a key, sees which field of the reader's output changed, and
compares the section's baseline for that key against what the reader leaves that same field
at when nothing is written. A schema field paired with the wrong setting fails here rather
than resolving one operator's value into another's setting. The probe has to differ from the
baseline, or the reader's output is identical either way and the check holds for a key
nothing reads; the check refuses a probe that does not.

**Before adding one.** Advancing coverage is normally wiring an existing check to another
section, and that is the first thing to try. A new check earns its place by naming a
failure none of the eight can see. A signature that changes twice, or a second check
proposed for a property one of these already touches, is the signal to redesign rather
than patch.

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
fuzzer. Seeds name their shape through the `fuzzing.Kind*` constants and go through
the recorder rather than `f.Add`, which is what lets the harness read back the
corpus it is being asked to trust:

```go
seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
seeds.AddRow(uint(idx), fuzzing.KindNil, "", int64(0), false)                 // absent-key path
seeds.AddRow(uint(idx), fuzzing.KindString, "not-a-number", int64(0), false)  // malformed input
```

At least one of a row's seeds has to resolve the row's field to something other
than what an absent key resolves it to, and
`CheckEveryRowHasADiscriminatingSeed` fails the section when none does. Where the
predicted leaf and the absent-key leaf agree, the row's assertion is comparing a
document against itself: it holds for a reader that resolves the key and equally
for one that never looks it up, so the key can be renamed in production with the
suite green. Nine of the ninety-six rows the suite then held were in that position when this
check was written.

The two seeds above do not settle it on their own. An unguarded row resolves an
absent key to its cast's zero, and so does any value that cast rejects on an
unchecked read, so both of those seeds land on the absent-key value and only a
value that converts to something else discriminates. A guarded row is undone the
other way, by a seed that happens to carry the in-code default.

Finally, record the row's key and review the line it adds:

```bash
go test ./evmrpc/config/ -run TestKeyNames -update
```

`Renaming a Key` below says what that record catches which the seeds cannot, and
why a row that reaches its key through the reader's own flag constant needs it.

Write one row per key, including when two keys land in the same struct field. The
manifest is what the differential enumerates, so a key with no row is a key the
comparison never makes.

`CheckManifestCoversEveryField` covers the weaker half of that automatically: every
resolved field must be named by some row's `Path` or `AlsoWrites`, or exempted at the
call site. It works on fields rather than keys, so it catches a field no row claims
and cannot catch a second key landing in a field some other row already claims. That
case is the one the per-key rule above exists for, and nothing mechanical will
prompt you.

Each exemption says which of two things it is, either a key driven by a dedicated target in
the same file or a field carrying no configuration key at all, and says it in a comment
beside the path. A bulk exemption is the same move as widening an assertion, in that it makes
the check green over the surface it was there to measure.

It assumes one reader per struct, which is what keeps it out of `[state-commit]`.
`StateCommitConfig` is populated by two readers that each read keys the other does not. One
is `parseSCConfigs` over the flat `AppOpts` map, the other is
`sei-cosmos/server/config.GetConfig` over viper, the only reader of four of the five
`state-commit.flatkv.*` keys. They are not disjoint, since eleven keys are read by both,
`sc-write-mode` and
`flatkv.enable-read-write-metrics` among them. So "every field of this struct is named by
`scKeys`" is not a true statement to assert, and making it pass would take 62 exemptions
against a 17-row manifest, four of them claiming a key is unread when the other reader
reads it.

Fifty-seven of those 62 sit under `FlatKVConfig`, so the move to reach for is waving the
subtree through in one line. Exemptions match a whole `Dump` path, so `"FlatKVConfig"`
exempts nothing and the count does not drop. Were it a prefix it would surrender the
protection the check exists for, since a new `state-commit.flatkv.*` key in
`parseSCConfigs` would go unflagged and that reader already reads one of them. The shape
that would work is per reader rather than per section. `defaults` is an `any`, so the check
can be pointed at `FlatKVConfig` alone inside `sei-cosmos/server/config` with that reader's
five flatkv keys as rows, and the 53 exemptions left would each say truthfully that the
field carries no configuration key. Unbuilt. Meanwhile, wire the check where the section
has one reader; a demotion is caught for every section by the record's marker regardless.

## Renaming a Key

Key names are recorded in `testdata/<section>.keys.golden`, one quoted key per line, and
compared by `CheckKeyNames` on every run. The order is the manifest's rows, then a
`# keys with a target of their own` marker, then any keys recorded for their name alone
(`A key with no row`). So the file is longer than the manifest has rows, and that is not
staleness. Trimming it to the row count deletes rows, which is one of the four forbidden
moves.
Renaming a key fails the comparison with the old and the new spelling in the report.
Regenerate and keep the diff in the review:

```bash
go test ./app/ -run TestKeyNames -update
```

The record exists because the seeds cannot cover this case, and the reason is worth
knowing before trying to satisfy one with the other. A row's assertions and the
discriminating-seed check both take the key from the row, so when the row names its
key through the same constant the reader passes to `appOpts.Get`, the way
`{Key: FlagSSImportNumWorkers}` does against `opts.Get(FlagSSImportNumWorkers)`, editing
that constant's value moves both halves together. Every assertion still passes, and
now passes about a key no node has ever been configured with. Thirty-one rows are
spelled that way, and editing that constant is exactly how an app.toml key gets
renamed. The count of rows is deliberately not stated here, because it moves whenever
anyone adds a key, and a number in prose goes stale silently where the records do not.

So the two checks divide the work, and which one fires tells you what happened:

- The reader's key moved and the row kept the old spelling: nothing can discriminate
  the row any more, so `CheckEveryRowHasADiscriminatingSeed` fails. It distinguishes
  this from badly chosen seeds by trying whether *any* value discriminates, and says
  which case it found, so a report naming the reader is not asking for another seed.
- The row and the reader moved together: the recorded name is the only copy that did
  not, so `CheckKeyNames` fails and the seeds stay green.

Neither is a substitute for the other. `CheckEveryRowHasADiscriminatingSeed` compares the
same record, so a section cannot acquire seeds without acquiring the record, and deleting
the `CheckKeyNames` call does not turn a rename back into a green run. Deleting a check to
clear a failure is the same move the four above forbid for rows. That is why a genuine
rename is reported twice, once from each check, with the same diff.

The tie runs one way only, and the direction is worth knowing before relying on it. Seeds
imply the record, and not the reverse. A section wired for `CheckKeyNames` alone, meaning one
with no manifest and so no seeds to check, acquires no seeds check, and nothing detects that
its one call has been deleted. `[state-sync]` in `cmd/seid/cmd` is such a section, because
`NewApp` reads its three keys into a baseapp, which no row can describe, so the record is all
that package has and the call holding it is deletable. The same three keys do have rows in
`sei-cosmos/server/config`, where `GetConfig` resolves them into a struct. What admits a row
is the reader rather than the section. Wire a manifest where a manifest is possible.

Renaming a key is a migration, and the record is not the whole of it. The app.toml
template that renders the old spelling (`sei-db/config/toml.go` writes
`ss-import-num-workers` as literal text), the flag registration and the documentation
are separate places holding the same string, and nothing here checks them. What the
record buys is that the rename cannot land without a reviewer seeing which
operator-facing key moved.

### A key with no row

Some keys cannot be described by a row at all. `sc-write-mode` panics on a value its
parser rejects, `sc-hash-logger-target-file-size` adopts a cast result only when it is
positive, `sc-write-mode-enable-auto` rewrites a second field through
`ApplyWriteModeAuto`. `CheckRow` would predict the wrong resolution for each, so each
has a fuzz target of its own that asserts it directly. Those targets spell the key
through the reader's own constant, which puts the name in exactly the position this
record exists for.

Record such a key as a `configtest.KeyName`, and pass the list to both checks:

```go
var scKeysWithTargetsOfTheirOwn = []configtest.KeyName{
    FlagSCWriteMode,                // FuzzSCWriteMode
    FlagSCWriteModeEnableAuto,      // FuzzSCWriteMode
    FlagSCHashLoggerTargetFileSize, // FuzzSCHashLoggerTargetFileSize
}

configtest.CheckEveryRowHasADiscriminatingSeed(f, "state-commit", readSC, scKeys, seeds,
    scKeysWithTargetsOfTheirOwn...)
configtest.CheckKeyNames(t, "state-commit", scKeys, scKeysWithTargetsOfTheirOwn...)
```

A `KeyName` is a distinct type from `KeySpec` so the compiler keeps it out of `CheckRow`,
`Pick` and the discriminating-seed check: it claims the spelling and predicts nothing
about the resolved value, and the target that can express the prediction keeps it. Both
call sites take the same list because both compare the whole record, so a list passed to
one and not the other fails on the next run rather than recording half of it. The names
go after the rows, which is what keeps adding one from rebinding the row index a
section's seeds select by.

`specs` may be nil where a package's reader admits no row. `[state-sync]` in `cmd/seid/cmd`
is recorded that way, because the reader there is `NewApp`. The record is then the marker and
the names, and read at a glance it says that nothing in that file is held to a resolved
value.

This is not a place to park a key a row could describe. A row is held to the resolution
on every value the fuzzer reaches; a `KeyName` states one string, which is the least the
suite can say about a key. The record enforces that distinction rather than trusting it:
the two halves are separated by a `# keys with a target of their own` line, so deleting a
row and re-recording the same key below the marker moves a line across it and fails. That
edit used to produce a byte-identical file, and because each one made the next row the last
one, a manifest could be emptied one green run at a time.

## Changing a Default

Section defaults are recorded in `testdata/<section>.golden` and compared on every
run. Regenerate the file and review the diff:

```bash
go test ./evmrpc/config/ -run TestDefaultsMatchTheRecordedValues -update
```

Pass `-update` per package rather than tree-wide. The flag is only registered in
binaries that link the harness, so `go test ./... -update` makes every other package
exit with "flag provided but not defined".

Name the package before the flag, as every command here does. `go test -run TestKeyNames
-update ./app/` puts an unrecognized flag ahead of the package list, so `go test` reads
`./app/` as an argument to the test binary and runs the package in the current directory
instead, which does not link the harness, and exits with the same `flag provided but not
defined: -update`. That failure looks like a broken check rather than a mis-ordered command.

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

A new section needs six things, and the last three are the ones that are easy to miss:

1. `CheckDefaults` against a checked-in `testdata/<section>.golden`.
2. `CheckAbsent`, asserting that a reader handed no keys returns exactly the
   package's declared defaults, unless the reader clobbers, in which case
   `CheckAbsent` would assert the opposite of the truth and the divergence gets a test
   that names it instead. `[state-sync]` in `sei-cosmos/server/config` is that case, where
   `snapshot-keep-recent` defaults to 2 and an absent key resolves 0.
3. A `[]KeySpec` manifest with a `CheckRow` fuzz target, seeded per row through
   `NewSeeds` and asserted by `CheckEveryRowHasADiscriminatingSeed`.
4. `CheckKeyNames` against a checked-in `testdata/<section>.keys.golden`, in a
   `TestKeyNamesMatchTheRecordedNames`. `CheckEveryRowHasADiscriminatingSeed` compares
   the same record, so a section that has the seeds cannot lack the record and deleting
   this call does not re-green a rename. The reverse does not hold (see `Renaming a Key`).
5. `CheckManifestCoversEveryField`, so the manifest cannot silently fall behind the
   reader, unless the section's struct has a second reader covering different keys.
6. `CheckWiring`, which is called once per package rather than once per section. A new
   section in a package that already calls it needs its coverage record regenerated
   instead of a new call. See `Recording the Wiring`.

The manifest has to be a package-level `var` for the fourth, since a table declared
inside its fuzz target is not something a `Test` function can name.

## Recording the Wiring

Every check above shares one blind spot, which is that deleting a call to it is silent. A
section covered by five checks and then by four still passes everything that remains. Two
instances were confirmed by experiment, in `evmrpc/config` and `giga/executor/config`, where
three calls were removed from a fully covered section and every package stayed green.

`CheckWiring` closes that. It reads the package's own test sources and records one line per
`(section, check)` pair in `testdata/wiring_coverage.txt`. A line reads as "this section is
covered by this check", so `evm CheckAbsent` means some test in the package calls
`configtest.CheckAbsent` naming `"evm"`. A line that disappears is coverage that was deleted,
and that is the failure the file exists to report.

This is the **coverage record**, the third of the suite's three record kinds, alongside the
defaults record (`<section>.golden`) and the key-names record (`<section>.keys.golden`). It is
compared exactly, the way `go.sum` is compared, so every line has to still be there. It is not a lint baseline, which records violations to ignore and wants to
shrink to nothing. This wants to stay complete, so adding a check also fails until the record
is regenerated, and the failure names what was added separately from what was removed.

```bash
go test ./<pkg>/ -run TestWiringMatchesTheRecord -update
```

Four properties are worth knowing before relying on it:

- It establishes that a call is **written**, not that it ran. The failure being prevented is
  a deleted line, and a call left in place but unreachable is a far more visible edit.
- Build tags are ignored on purpose. Honoring them would make the record differ between a
  Linux CI runner and a local machine, so a record generated on one could not be compared on
  the other.
- It records the literal text of the section argument, so a package spelling one section two
  ways shows up as two sections. That is a thing to notice in the diff rather than something
  the check can decide.
- Deleting the `CheckWiring` call itself is the one deletion it cannot report, so
  `TestEveryWiredPackageRecordsItsWiring` in `testutil/configtest` asserts from one place that
  every package calling a check calls it too. That assertion finds a package by finding a
  check in it, so deleting every check in a package at once drops it from the set and orphans
  its record. Removing whole test files is conspicuous enough to leave to review.

## Running

```bash
go test ./testutil/... ./app/ ./evmrpc/config/          # the suites, ordinary run
go test -race ./<pkg>/                                  # the bar CI holds
go test ./<pkg>/ -run FuzzXxx -fuzz FuzzXxx -fuzztime 60s   # extend a target by hand
```

Seeds cover every row on an ordinary run. Running the fuzzer by hand explores past
the seeds and is worth doing when changing a reader's cast or its guard.

## Comparing two resolved vipers

Two managers, two boots, or a before/after both reduce to "did these resolve the same
values". Compare `Settings`, and report with `DumpViper`.

- `configtest.Settings` is the value you assert on: a flat map from dotted key to what
  `Get` returns, one entry per `AllKeys` entry. Use it, not `Viper.AllSettings`.
  `AllSettings` re-nests the flat key space by splitting on `.`, so when one key is a
  dotted prefix of another it drops one of them depending on map iteration order, and
  an equality assertion between two of them can fail on identical input. It also omits
  every key whose `Get` returns nil, so a key one side enumerates and resolves to
  nothing looks the same as a key the other side never enumerated at all.
- `configtest.DumpViper` is for the failure message, not the assertion. It renders one
  sorted, type-qualified line per key, which is what a human reads, but it joins with
  newlines and does not quote keys, and a TOML key may legally contain a newline, so two
  different key sets can render identically. Assert on the map; print the dump.
- `Settings` panics on a nil viper, deliberately. A `server.Context` carries no viper
  until `Apply` populates one, and a nil-tolerant comparison would let two contexts
  nobody populated compare equal and report a parity that was never established.

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

## Proving a Test Would Fail

A suite that passes says nothing about whether it would fail. The other question is what this suite
is for: take a claim the code makes, remove it, and name the test that objects. `scripts/mutation-check.sh`
runs one such check and reports `CAUGHT by`, `SURVIVED`, or `INVALID MUTATION`.

```bash
scripts/mutation-check.sh <file> <old-text-file> <new-text-file> <package>...
```

Five properties it enforces, each written down because getting it wrong reports a false result and
every one of them has produced one:

1. **The baseline is green first.** A suite already failing reports every mutation as caught, because
   the failure was there before the mutation was.
2. **The mutation applied.** A stale anchor leaves the code unmutated, so the run tests the original
   and reports a pass as a survived mutation.
3. **Compile status comes from the compiler.** Deciding it by grepping test output for phrases like
   `cannot use` misreads a test whose own failure message contains that phrase. A real catch then reads
   as a build error, and the claim goes unverified while looking checked.
4. **The restore is verified, not assumed.** An unrestored mutation poisons every run after it.
5. **A survivor is a decision, never a pass.** It is an untested claim, and a test is owed, or an
   equivalent mutant that changed no observable behaviour. Say which. Recording a survivor as
   "verified" is how a suite comes to describe code it does not hold.

One more that no script can check. A mutation is only worth running against a test that could
distinguish it. A section registered at the defaults a node already runs cannot tell an install from a
no-op, so a mutation removing the install survives against it for a reason that has nothing to do with
the test being weak. When a survivor looks impossible, suspect the fixture before the code: the
fixture has to make the behaviour observable.

## Out of Scope

The suite covers the viper resolution and the keys `app.New` reads back out of the
flat map. Three classes sit outside it:

- Reads that need a running node, tracked in PLT-851.
- Direct environment and file reads that bypass the boot path entirely, such as
  `UPGRADE_VERSION_LIST` in `app/upgrades.go`. These carry their own tests and
  migrate onto the configuration SDK per call site.
- A file-layer comparison. The rows drive a reader at the `AppOpts` map level, which
  is the layer the new manager has to reproduce, so nothing here asserts that a value
  written as TOML resolves the same way as the same value handed over as a map. That
  axis would need a value-to-TOML renderer, and it is unbuilt.

`CheckDeclaredSurface` overlaps `CheckDefaults` on purpose, and the overlap is not the point
of it. `CheckDefaults` anchors one section's own defaults struct, and twelve of the fourteen
declared sections have one; the two that do not are the upstream sections in
`config/cosmosbase`, whose defaults belong to sei-cosmos and would be a second copy of the
same numbers if recorded per section. For those, the surface record is the only independent
anchor. For the rest it is a second one, and it is the only record that shows the whole key
space in one diff, which is what a reviewer needs when a change touches several sections.

It records text rather than the hash of it deliberately. `Fingerprint` is the cheap thing a
deploy compares; the surface is what a human reads when the comparison fails. A recorded hash
fails with no way to see what moved.

One channel can be refused for a key rather than resolved, and it is the only place this
surface knowingly diverges from the machinery it replaces. An environment variable carries
one string, and a reader that asserts its value's exact type rather than casting cannot be
handed one: the metric label set is that case, and resolving a variable for it installs a
value that stops the node. `registry.RefuseFromEnvironment` leaves the key out of the
environment layer with a stated reason, so the file's value applies and the node starts
where the legacy path refuses. The reason is required, because the trade is a refused boot
for a setting that silently does nothing, and the second is only acceptable if somebody is
told. `doctor` reports the variable as ignored, and a boot test holds that the node comes up
with it set.
