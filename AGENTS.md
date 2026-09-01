# Sei Chain

`github.com/sei-protocol/sei-chain` is a Cosmos SDK / Tendermint blockchain with
a native EVM. It targets **Go 1.25.6**.

## Nested guides

When working within a specific package, always check for and read any `AGENTS.md`
file in that package directory (and its parent directories) before making
changes. These contain domain-specific architecture decisions, conventions, and
constraints that supplement this top-level guide. Context increases
progressively the deeper you go. Existing package guides include:

- `evmrpc/AGENTS.md` — EVM JSON-RPC (`eth_*`, `sei_*`, `debug_*`) semantics
- `x/evm/AGENTS.md` — EVM module: address association, StateDB bridge, precompiles, pointers
- `sei-tendermint/AGENTS.md` — sei-tendermint module conventions

## Code style

All Go files must be both `gofmt`- and `goimports`-compliant (`.golangci.yml`
enables the `gofmt` and `goimports` formatters). After modifying **any** `.go`
file, run **both** tools on **every** file you touched — not just the ones you
think changed formatting:

```bash
gofmt -s -w <file>...
goimports -w <file>...   # groups/orders imports; catches the goimports linter
```

`goimports` is required in addition to `gofmt`: `gofmt` alone does not separate
the stdlib import group from third-party imports, so a `gofmt`-clean file can
still fail the `goimports` linter.

Verify the whole tree with the check CI gates on, which prints nothing when
everything is clean:

```bash
make fmtcheck   # golangci-lint fmt --diff
```

Prefer it over a bare `goimports -l .`, which also reports generated files that
the formatters are configured to skip. Note that `golangci-lint run` will **not**
catch a misformatted test file: it honours `run.tests`, which is false, so its
formatters never see one. `fmtcheck` is a separate invocation for that reason.

### Godoc

Godocs say **what** a thing is, not why it came to be or how it works inside.

1. **Explain WHAT, not WHY or HOW.** Rationale, trade-offs, and mechanism belong in
   an inline comment at the line that needs them, or nowhere.
2. **Never record design history.** No "this was previously X", "used to mean Y",
   "renamed from Z". The diff and the git log hold that.
3. **Multi-paragraph godocs are rare.** Most functions do not earn a second
   paragraph. One or two sentences is the norm.
4. **Rewrite, don't patch.** When a godoc needs to change, write it again from
   scratch; incrementally editing one reliably produces a rambling comment.
5. **Document the subject, not the system.** A godoc is not the place to explain
   the surrounding architecture. Describe this function, type, or field.

```go
// ❌ BAD — history, mechanism, and a system tour
// GetRollbackFloor returns the earliest height a rollback may target. The window is
// measured against the store's own head rather than a height handed down, because the
// collector takes a minimum across stores, so a lagging store sets the depth. 0 means
// nothing is eligible; it is a height rather than a sentinel, since CannotServeRollback
// used to serve that role and was removed. Answering high is the damaging direction,
// as nothing above clamps it: the collector derives its cut lines from these answers.
func (s *blockDB) GetRollbackFloor(rollbackWindow uint64) uint64

// ✅ GOOD — what it returns, and what the caller must know
// GetRollbackFloor returns the earliest height a rollback may target, measured against
// this store's own head. It returns 0 when the window is deeper than the store's
// history, meaning no data here is eligible for pruning.
func (s *blockDB) GetRollbackFloor(rollbackWindow uint64) uint64
```

## Structural corrections

When a defect is found, the change that closes it has to leave the code readable as a
sequence of named steps a new engineer can follow top to bottom without someone
narrating it. Three rules make that checkable, and they are what review looks for.

**Guard at the choke point, never at each caller.** A guard repeated at every call
site is a convention the next caller can forget, where a guard at the single function
every path passes through is an invariant they cannot. When there is no such function,
that is evidence the abstraction is wrong, so say so rather than distributing the
guard.

*Worked example, from the configuration record refusal.* The refusal to rewrite a record
on CI lives inside `writeGolden`, the one function every record write passes through, so a
record writer added later is covered without anyone having to remember it. One caller keeps
a guard of its own, `requireKeyNameRecord`, and the reason is the distinction to copy: it
suppresses a comparison rather than performing a write, and a suppressed comparison reads
as a pass.

**The step name carries the *what*, and the doc comment carries the *why*.** A long
comment sitting inline in a flow means the step was never named. Extract the step and
move the rationale to its doc comment. Relocating a load-bearing invariant is the
move, never deleting one to tidy up.

**Behaviour never changes in a readability refactor,** and the proof is the existing
tests passing *unchanged*. A refactor that requires editing a test is not a refactor.

After restructuring, re-read the result top to bottom as that sequence of named steps, and
check each step against the shapes the surrounding package already uses rather than a pattern
introduced for this one change. None of this is checked by a linter, which is why it is
written down. The failure it prevents is a codebase where found problems accumulated fixes
instead of corrections, and that is indistinguishable from a healthy one on any green test
run.

## Lint, build & test

Linting and formatting are driven by the root `Makefile` and `.golangci.yml`
(golangci-lint v2.8.0; enabled linters include `errcheck`, `gosec`, `govet`,
`staticcheck`, `ineffassign`, `goconst`, `prealloc`, `unconvert`, `misspell`,
`bodyclose`, and `dogsled`; generated `*.pb.go` files are excluded).

```bash
make lint     # golangci-lint run + golangci-lint fmt + go vet ./... + go mod tidy + go mod verify
make fmtcheck # report what the formatters would rewrite, without rewriting it (CI gates on this)
make dblint   # same checks scoped to ./sei-db/... (faster when iterating there)
make build    # build the seid binary into ./build/seid
make install  # install seid into $GOBIN
```

Tests run with the race detector and coverage. CI shards them into groups; while
iterating, run a single package directly:

```bash
make test-group-0       # one CI test shard (race + coverage)
go test ./<pkg>/...     # run a single package
```

CI mirrors these checks: `.github/workflows/golangci.yml` runs golangci-lint
v2.8.0 followed by `golangci-lint fmt --diff`, and `.github/workflows/go-test.yml`
runs `go test -race` on Go 1.25.6.

### Running tests on a RAM disk

If you are running tests that use on-disk resources, consider using a RAM disk to
speed it up. Tests under sei-db/* are very likely to benefit from this. Other tests
may or may not benefit depending on disk utilization. Tests that do not use on-disk
resources are unlikely to experience significant benefit from using a RAM disk.

`scripts/ramtest.sh` runs `go test` with `GOTMPDIR` and `TMPDIR` on a RAM-backed
filesystem. Arguments that are not its own flags pass through to `go test`, so
package patterns, `-run`, `-count`, `-parallel` and `-v` work as usual, and relative
patterns resolve from the directory you run it in. `--help` lists the flags. Works on
macOS and Linux; on Linux it prefers `/dev/shm`, falls back to a sudo-mounted tmpfs
where that is too small, and warns and runs unaccelerated when neither is available.

```bash
scripts/ramtest.sh ./sei-db/...
scripts/ramtest.sh ./sei-db/state_db/sc/flatkv/... -run TestSnapshot -v
scripts/ramtest.sh                         # whole repo (./...)
scripts/ramtest.sh --keep ./sei-db/...     # leave the volume up for the next run
scripts/ramtest.sh --down                  # release the RAM disk, not needed for clean run
```

**Memory.** A full `./sei-db/...` run needs ~9 GiB free: ~5.5 GiB of test data plus
~3 GiB of concurrent test binaries. Run subtrees on a smaller host. `--size N` (GiB)
overrides the default `clamp(RAM/2, 4, 32)`, but it is a ceiling rather than a
reservation, so raising it neither costs nor relieves memory. On the Linux `/dev/shm`
path the request is clamped down to what that tmpfs actually has free, with a warning.
Size from the peak each run reports.

- Exit 3 means the RAM disk filled, not a test failure. Retry with a larger `--size`.
- Exit 4 means the volume could not be released and its memory is still reserved. The
  message names the command that frees it.
- `peak use: 0` means nothing reached the RAM disk: either the run wrote nothing, or the
  redirect did not take effect and the run was not accelerated.
- `--keep` is sticky: a run only tears down a volume it created, so once you keep one
  you own the teardown. `--down` releases it from any later shell.
- macOS volumes are case-sensitive (HFSX) unlike the APFS root. A new path-case
  failure is a real bug, not a script problem.
- Not CI parity: `-race` is off by default, and `--ci-tags` is needed for the ledger
  tests. A green run here is not a green CI run.
- Each worktree gets its own volume, so parallel agents do not collide. Within one
  worktree only one run at a time: a second is refused, because both would share the
  volume and wipe each other's scratch directories.

## Benchmarking

See [`benchmark/CLAUDE.md`](benchmark/CLAUDE.md) for benchmark usage, environment
variables, and comparison workflows.
