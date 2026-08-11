# Sei Chain

`github.com/sei-protocol/sei-chain` is a Cosmos SDK / Tendermint blockchain with
a native EVM. It targets **Go 1.25.6**.

## Nested guides

When working within a specific package, always check for and read any `AGENTS.md`
file in that package directory (and its parent directories) before making
changes. These contain domain-specific architecture decisions, conventions, and
constraints that supplement this top-level guide. Context increases
progressively the deeper you go. Existing package guides include:

- `evmrpc/AGENTS.md` — EVM JSON-RPC (`eth_*`, `sei_*`, `sei2_*`, `debug_*`) semantics
- `x/evm/AGENTS.md` — EVM module: address association, StateDB bridge, precompiles, pointers
- `sei-tendermint/AGENTS.md` — sei-tendermint module conventions
- `testutil/configtest/AGENTS.md` — configuration characterization: how to pin a new key, section, or default

## Configuration reads

How a seid node resolves configuration is pinned by the characterization suite in
`testutil/configtest`. Renaming a key the suite covers, changing a default, or changing
how a value is cast will fail that suite. **Adding** a key does not always: the completeness
check compares struct fields, so a second key landing in a field some row already
claims is uncaught and the row has to be written by hand. Where there is a failure it
is the review prompt: record the new behavior so the old and new value land in a diff,
rather than skipping the row or widening the assertion until it passes. Read
[`testutil/configtest/AGENTS.md`](testutil/configtest/AGENTS.md) before changing a
configuration read, and before adding one.

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

Verify the whole tree (each prints nothing when everything is clean):

```bash
gofmt -s -l .
goimports -l .
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
make lint     # golangci-lint run + go fmt ./... + go vet ./... + go mod tidy + go mod verify
make dblint   # same checks scoped to ./sei-db/... (faster when iterating there)
make build    # build the seid binary into ./build/seid
make install  # install seid into $GOBIN
```

Tests run with the race detector and coverage. `go-test.yml`'s Race Detection job
shards into `NUM_SPLIT` (currently 3) parallel matrix jobs, round-robin split
(package `i` goes to shard `i % NUM_SPLIT`, not a contiguous chunk — see the
`split-test-packages` Makefile target). `make test-group-N` reproduces a given
CI shard locally with the same package split (`NUM_SPLIT` defaults to 3):

```bash
make test-group-0   # reproduce CI race shard 0 locally
go test ./<pkg>/...  # run a single package
```

CI mirrors these checks: `.github/workflows/golangci.yml` runs golangci-lint
v2.8.0 and `.github/workflows/go-test.yml` runs `go test -race` on Go 1.25.6.

## Benchmarking

See [`benchmark/CLAUDE.md`](benchmark/CLAUDE.md) for benchmark usage, environment
variables, and comparison workflows.
