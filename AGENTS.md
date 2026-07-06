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

Tests run with the race detector and coverage. CI shards them into groups; while
iterating, run a single package directly:

```bash
make test-group-0       # one CI test shard (race + coverage)
go test ./<pkg>/...     # run a single package
```

CI mirrors these checks: `.github/workflows/golangci.yml` runs golangci-lint
v2.8.0 and `.github/workflows/go-test.yml` runs `go test -race` on Go 1.25.6.

## Benchmarking

See [`benchmark/CLAUDE.md`](benchmark/CLAUDE.md) for benchmark usage, environment
variables, and comparison workflows.
