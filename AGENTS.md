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

All Go files must be `gofmt`-compliant. After modifying any `.go` file, run:

```bash
gofmt -s -w <file>
```

Or verify the whole tree (prints nothing when everything is clean):

```bash
gofmt -s -l .
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

## Cursor Cloud specific instructions

The startup update script only runs `go mod download`. Build the node yourself
with `make install` (outputs `~/go/bin/seid`); it is not part of the update
script. Add `~/go/bin` to `PATH` before invoking `seid`.

Run a local single-node chain with `./scripts/initialize_local_chain.sh` (see
`scripts/initialize_local_chain.sh`). It runs `make install`, wipes `~/.sei`,
inits chain-id `sei-chain` with a pre-funded `admin` key (keyring backend
`test`), and then `seid start`. Do not run it in the foreground of a
short-lived shell — start it in a long-lived tmux session, since `seid start`
runs until killed.

Gotcha: the script tests `$NO_RUN` unquoted under `set -e`, so `NO_RUN` MUST be
set or the script errors before starting. Use `NO_RUN=0 ./scripts/initialize_local_chain.sh`
to init and start, or `NO_RUN=1 ./scripts/initialize_local_chain.sh` to only
init (then `seid start --chain-id sei-chain` yourself).

Default endpoints after startup: Tendermint RPC `:26657`, Cosmos REST `:1317`,
gRPC `:9090`, EVM JSON-RPC HTTP `:8545`, EVM WS `:8546`. EVM `eth_chainId`
returns `0xae3f2`.

Quick smoke test once running: `seid status --node tcp://localhost:26657`,
`curl -s -X POST http://localhost:8545 -d '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}'`,
and a bank send `seid tx bank send admin <addr> 1000000usei --chain-id sei-chain --keyring-backend test --fees 20000usei -y`.

`make lint` runs golangci-lint over the whole tree plus `go mod tidy`/`verify`
(slow); scope to a package with `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run ./<pkg>/...`
while iterating.
