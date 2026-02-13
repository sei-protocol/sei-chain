# AGENTS - Sei Chain (Monorepo)

This document is the primary orientation for automated agents operating in
`/Users/pdrobnjak/sei/sei-chain`.

It reflects the repository as discovered in this working copy and is intended for
Go-first workflows.

## Scope and project shape

- This repo is a Go workspace defined in `go.work` with these active modules:
  - `.` (main `sei-chain` module)
  - `./sei-cosmos`
- Additional sibling modules include:
  - `./sei-tendermint`
  - `./sei-ibc-go`
  - `./sei-wasmd`
  - `./sei-wasmvm`
  - `./sei-db`
  - `./oracle/price-feeder`
  - `./sei-iavl`
- Do not assume one universal command for all modules; treat each module as having
  its own `Makefile` contract where noted.
- Go toolchain expectations are documented in `go.mod`/`go.work` files as
  `go 1.25.6` for main and workspace modules.
- This document aligns with the root `CLAUDE.md` guidance and repository-specific
  operational notes.

## Build / lint / test command matrix

### Root module (`sei-chain`)

- `make install`
  - Installs `./cmd/seid` using module build tags and `-ldflags` from root
    `Makefile`.
- `make build`
  - Build binary to `./build/seid`.
- `make install-mock-balances`, `make install-bench`,
  `make install-with-race-detector`
  - Alternate build variants used in specific CI/dev workflows.
- `make lint`
  - Runs `golangci-lint`, gofmt check (`gofmt -d -s`) and `go mod verify`.
- `make test-group-<N>`
  - Uses `NUM_SPLIT` (default 4) and package bucketing.
  - Example: `make test-group-0` or `NUM_SPLIT=8 make test-group-3`.
- `make clean`
  - Removes `./build` artifacts.

### Submodule commands that are routinely used

- `make -C sei-cosmos test` (standard)
- `make -C sei-cosmos test-all`
- `make -C sei-cosmos lint`
- `make -C sei-cosmos test-unit`
- `make -C sei-cosmos test-race`
- `make -C sei-cosmos test-cover`

- `make -C sei-ibc-go test`
- `make -C sei-ibc-go test-all`

- `make -C sei-wasmd test` (this target delegates to `test-unit`)
- `make -C sei-wasmd lint`
- `make -C sei-wasmd test-cover`

- `make -C sei-tendermint test`
- `make -C sei-tendermint test-race`

- `make -C sei-db test-all`
- `make -C sei-db lint-all`

- `make -C oracle/price-feeder test-unit`
- `make -C oracle/price-feeder lint`

- `make -C sei-iavl test`

### CI-parity baseline commands

These are the command patterns currently used in GitHub workflows:

- `go test -race -tags='ledger test_ledger_mock' -timeout=30m ./...`
  (root + sei-cosmos modules in CI).
- `go test -tags='ledger test_ledger_mock' -timeout=30m -covermode=atomic -coverprofile=coverage.out -coverpkg=./... ./...`
  (coverage jobs).
- `go test -mod=readonly` appears throughout module makefiles; prefer this flag for
  CI-grade test and lint parity.
- `go mod verify` runs in lint/verification flows and should not be skipped when
  checking dependency integrity.

### Single-test and focused test commands

Use these patterns for quick iteration:

- Single test in a package:
  - `go test -mod=readonly ./app -run TestStateMachine -count=1`
- Single test with tags used by this repo:
  - `go test -mod=readonly -tags='ledger test_ledger_mock' ./app -run TestStateMachine -count=1`
- Single package with race detector:
  - `go test -mod=readonly -race ./app -run TestStateMachine -count=1`
- Single test file style filter (regexp):
  - `go test ./x/oracle/... -run '^Test.*Price.*$'`
- Single module focus example:
  - `go test -mod=readonly -tags='cgo ledger test_ledger_mock' ./...`
    (when run from `sei-cosmos` or `sei-ibc-go`).

If your change affects a single module, prefer running the command in that module's
directory to avoid long cross-module test suites.

## Required formatting and import style

- Always keep code formatted with standard gofmt (simplify mode required).
- All Go files must be gofmt compliant. After editing any `.go` file:
  - `gofmt -s -w <file>`
- Quick check:
  - `gofmt -s -l .`
- Before commit, run style checks used by project lint:
  - `gofmt -w -s` style via make targets, and `goimports` where configured.
- Use import grouping with stdlib first, a blank line, then third-party/internal.
- Keep local import aliases consistent and avoid unnecessary aliases unless naming
  collisions require them.
- Preserve generated file exemptions noted in make rules (for example proto or statik
  artifacts) unless the change explicitly regenerates them.
- For deterministic local formatting in Go files, rely on tooling instead of manual
  alignment.

## Naming and type conventions

- Exported identifiers:
  - Use `CamelCase` / `PascalCase`.
  - Keep acronyms in conventional block style (`ID`, `URL`, `JSON`) unless codebase
    has a local historical exception.
- Unexported identifiers:
  - Use `camelCase`.
- Types and function names should describe domain intent, not implementation detail.
- Prefer concrete types over `interface{}` at package boundaries.
- Use enums / constants for stringly-typed domains when possible.
- Favor struct field names that mirror domain semantics (`WindowSize`, `MaxItems`, etc.)
  and are self-documenting without excessive comments.
- Keep receiver names short and consistent per type (`lg`, `cfg`, `app`, `tm`).
- Prefer constructor functions (`NewXxx`) that return immutable/configured values and
  validation errors early.

## Error handling conventions

- Return `error` values instead of panicking for expected runtime failures.
- Wrap context with `%w` when adding call-site context so call stacks remain
  inspectable with `errors.Is`/`errors.As`.
- Check and branch on sentinel errors where needed (for example `errors.Is(err,
  ErrX)`).
- Use `errors.New` for static messages and `fmt.Errorf("...: %w", err)` for
  wrapped propagation.
- Keep early returns readable:
  - validate inputs first, then execute happy path.
- Do not suppress errors in deferred blocks unless explicitly documented and
  intentionally transformed.

## Testing conventions

- Table-driven tests are preferred for behavior matrices.
- Use helpers marked with `t.Helper()`.
- For one-off deterministic failures, use `t.Fatalf` / `t.Errorf` with concise
  context.
- The codebase commonly uses `require` and `assert` from Testify; use them
  consistently in new tests.
- Add focused `-run` regex coverage for single regression checks during development.
- Include race coverage on concurrent code paths when feasible (`-race`).
- Avoid flaky wall-clock-based assertions; use deterministic clocks or fake time
  helpers where available.

## Test and benchmark hygiene

- Prefer running test suites with short timeouts for focused local runs,
  and increase for simulation/integration paths.
- For long-running or expensive suites, mark with package-appropriate tags and
  document why.
- Keep benchmark changes isolated; use dedicated `benchmark` targets where present.
- Treat integration/e2e commands as higher-cost and run sparingly in local
  developer flow unless specifically touching integration surfaces.

## Linting details and practical guidance

- `golangci-lint` is the primary static checker.
- `line length`, `complexity`, and deep lints are intentionally controlled by
  repo config; follow existing file patterns if introducing logic that might trigger
  `prealloc`, `ineffassign`, `errcheck`, `govet`, `staticcheck` and `gosec`.
- If lint reports style-only import order/formatting drift, run the formatter and
  rerun lint before pushing.
- `make lint` in module makefiles often includes both tool install/run and format
  validation; if it fails on newly modified files, re-run just formatting first
  then rerun lint.

## Dependency and module hygiene

- Keep module boundaries explicit and run module-local checks with `-mod=readonly`.
- Use `go mod verify` to validate module cache integrity when touching
  dependency-sensitive code paths.
- Do not edit dependency files casually; rely on standard `go` workflows.

## Files and directories commonly edited with caution

- Avoid manual edits in generated/compiled output directories
  (protobuf, statik, vendored-like generated assets) unless regeneration is the
  explicit change request.
- Favor minimal diff footprints in ABI/codec-related files.
- Keep test fixtures deterministic and preferably immutable once committed.

## Cursor/Copilot rules

- No `.cursor/rules/`, `.cursorrules`, or `.github/copilot-instructions.md`
  files were found in this repository.
- If that changes in the future, update this document before large agentic runs.

## AGENTS.md and CLAUDE.md convention

- All agent-facing instructions must live in `AGENTS.md` files, never in `CLAUDE.md`.
- Each `CLAUDE.md` must contain only a reference to its co-located `AGENTS.md` (e.g., `See [AGENTS.md](AGENTS.md)`).
- When adding new agent instructions for a directory, create or update the `AGENTS.md` in that directory and ensure a corresponding `CLAUDE.md` exists that points to it.
- Do not put instructional content directly in `CLAUDE.md` files.

## Quick pre-change checklist for agents

1. Identify target module(s) from changed paths.
2. Run module-appropriate format/lint commands after edits.
3. Run focused tests first; then expand to module-wide `test`/`test-all` as needed.
4. Add/adjust single-test run commands when narrowing regressions.
5. Re-check `go mod`/dependency integrity if modules are changed.

## File references used for this guide

- `Makefile`
- `sei-cosmos/Makefile`
- `sei-ibc-go/Makefile`
- `sei-wasmd/Makefile`
- `sei-db/Makefile`
- `oracle/price-feeder/Makefile`
- `.golangci.yml`
- `go.mod`, `go.work`
- `.github/workflows/go-test.yml`
- `.github/workflows/go-test-coverage.yml`
- `README.md`
