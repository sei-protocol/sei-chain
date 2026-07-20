# precompile_tests

Self-contained module for verifying Sei's **custom EVM precompiles** end-to-end
against a live Sei chain. Sibling of `integration_test/rpc_tests/` and built on
the same conventions (bootstrap-once state seeding, one spec file per subject,
serial single-process run, mochawesome reports); like that suite it deliberately
imports nothing from top-level test utilities — everything it needs lives under
`integration_test/precompile_tests/` with its own `package.json`, `tsconfig.json`
and Hardhat compile config.

## Scope boundary vs. rpc_tests

- **rpc_tests** owns *how precompile transactions surface through `eth_*`
  endpoints* (receipt schema, logs, gas fields, blocks).
- **precompile_tests** (this suite) owns *precompile method semantics*: per-method
  behavior, error handling, and Cosmos-side state effects.

Don't duplicate an assertion across the boundary — e.g. the staking-precompile
`Delegate` log shape in `eth_getLogs` belongs to rpc_tests, while "delegate
actually creates a delegation the staking module can see" belongs here.

## What this suite proves

There is no geth reference for precompiles (they are Sei-only, like `sei_*` RPC
methods). The **parity oracle is the chain itself**: EVM-side effects and
precompile-reported values are asserted against Cosmos-side queries (bank
balances, associations, …) over RPC/REST. For every precompile, the spec file in
`precompiles/` answers:

- **Happy path & state parity.** The method works and its effect/answer matches
  the Cosmos-side truth.
- **Error handling.** Bad input reverts (and out-of-gas failures trace as
  `execution reverted`, never as a Go panic — a consensus-relevant guard
  inherited from the legacy suite).
- **Dispatch semantics.** Real `CALL` / `STATICCALL` / `DELEGATECALL` from
  deployed contract bytecode (via the `PrecompileCaller` fixture) behave
  correctly: view methods respond under STATICCALL, state-changing methods
  reject readOnly and delegatecall contexts, payable rules hold. This is the
  layer the Go unit tests under `precompiles/` cannot exercise — they invoke
  executors directly and bypass interpreter dispatch.

ABIs are loaded from the repo's own `precompiles/<name>/abi.json` (the files the
chain binary embeds), so specs can never drift from the deployed interface.

### Current coverage (phase 1)

| Precompile | Spec |
|---|---|
| bank (0x…1001) | `precompiles/bank.spec.ts` |
| addr (0x…1004) | `precompiles/addr.spec.ts` |

Planned next (wasm-free first): staking, gov, distribution, json, p256, oracle
(retirement assertion), pointer (`addNativePointer`), pointerview. Wasm-gated
flows (wasmd, pointer `addCW*`, solo CW claims) follow in a separate `wasm/`
spec dir behind a live `isWasmEnabled()` check, since wasm deployments are
blocked on production chains. The ibc precompile is out of scope.

## Layout

```
integration_test/precompile_tests/
├── package.json                # module deps + scripts (compile / precompile:* / test:precompile)
├── tsconfig.json               # TypeScript config for the module
├── hardhat.config.ts           # compile-only config: contracts/ -> artifacts/
├── contracts/                  # PrecompileCaller.sol (CALL/STATICCALL/DELEGATECALL fixture)
├── .mocharc.bootstrap.json     # runs _start/ sequentially
├── .mocharc.run.json           # mocha config for the spec run (single process)
├── scripts/run-ci.sh           # orchestrator: deps + compile + wait-for-chain + bootstrap + run + merge
├── config/endpoints.ts         # env-driven endpoints
├── utils/
│   ├── constants.ts            # HD path, WEI_PER_USEI, docker devnet constants
│   ├── format.ts               # regex matchers: ADDRESS / HASH32 / SEI_ADDRESS / …
│   ├── chainUtils.ts           # provider + raw JSON-RPC + waitUntil
│   ├── evmUtils.ts             # EvmAccount + funding + association + contract deploy
│   ├── cosmosUtils.ts          # bank query oracle + admin funding/association
│   ├── precompileUtils.ts      # address book + ABI loading + revert/trace assertions
│   └── testUtils.ts            # typed RuntimeState + claimPool
├── runtime/                    # gitignored, holds runtime.json
├── _start/
│   └── 00_bootstrap.spec.ts    # one-time setup
└── precompiles/                # the actual specs (one file per precompile)
```

New precompiles just need a `<name>.spec.ts` in `precompiles/`; the runner picks
up any `*/*.spec.ts` automatically.

## Runner (recommended)

```bash
cd integration_test/precompile_tests
npm install && npm run compile     # one-time
# start your local Sei devnet first (e.g. `make docker-cluster-start` from the repo root)
npm run precompile:ci              # == bash scripts/run-ci.sh
```

`scripts/run-ci.sh` is the single orchestrator, used both locally and by the
`EVM Precompiles` matrix entry in `.github/workflows/integration-test.yml`. It
assumes a Sei EVM RPC is already reachable (the workflow boots the 4-node
cluster; locally you start it yourself), then end to end:

1. Installs deps (`npm ci`; skip with `SKIP_NPM_CI=true`) and compiles contracts.
2. Waits for the Sei EVM RPC on `:8545` and for the chain to be producing blocks.
3. Runs `precompile:bootstrap` then `precompile:run` in a single mocha process.
4. Merges the per-phase mochawesome JSON into one combined HTML report at
   `reports/merged/precompile-tests.html`.

Knobs: `SEI_EVM_RPC`, `SEI_TIMEOUT`, `SKIP_NPM_CI`.

## Running manually

All commands run from `integration_test/precompile_tests/`.

```bash
# 1. Make sure a local Sei node is up on http://localhost:8545 (the project's
#    usual local devnet, e.g. `make docker-cluster-start` from the repo root).

# 2. Run the suite (single mocha process).
npm run test:precompile      # bootstrap + run, recommended
# or, piecewise:
npm run precompile:bootstrap # writes runtime/runtime.json
npm run precompile:run       # runs every precompiles/*.spec.ts via .mocharc.run.json
```

> **Why a single process.** Every spec shares the one Sei chain and the
> bootstrap's funded-account pool (`claimPool` hands out disjoint slices via a
> module-level cursor, which is only correct in-process), so the suite runs
> serially.

Individual files can be run with `mocha` (which picks up `tsx` via `.mocharc`):

```bash
npx mocha --require tsx precompiles/bank.spec.ts
```

…but only after `npm run precompile:bootstrap` has produced `runtime/runtime.json`.

## Configuration

| Variable | Default |
| --- | --- |
| `SEI_EVM_RPC` | `http://localhost:8545` |
| `SEI_COSMOS_RPC` | `http://localhost:26657` |
| `SEI_REST` | `http://localhost:1317` |
| `SEI_ADMIN_MNEMONIC` | unset (a random admin is minted and funded via the docker devnet) |
| `PRECOMPILE_TESTS_RUNTIME_STATE` | `runtime/runtime.json` |
| `PRECOMPILE_POLLING_INTERVAL_MS` | `100` (Sei blocks are ~400ms; ethers default 4s is too slow) |

## Authoring a new spec

Structure every spec into the three sections (happy path & state parity /
error handling / dispatch semantics). Rules of the road:

1. **Read-only at runtime.** Bootstrap is the only writer of `runtime.json`. If
   you need new pre-computed state, add it to the `RuntimeState` interface and
   populate it in `_start/00_bootstrap.spec.ts`.
2. **Pool accounts are single-use.** Claim fresh accounts with
   `claimPool(runtime, provider, count, label)` (testUtils), which hands out a
   disjoint slice of `runtime.funded.pool` on every call; never reuse a pool key
   across specs. Association is permanent, so anything a spec associates must be
   a `EvmAccount.random()` wallet or a pool account claimed by that spec.
3. **No imports from repo-level test utilities** — keep this module self-contained.
4. **Assert against the Cosmos oracle.** A state-changing method isn't verified
   by its receipt alone; check the effect through `cosmosUtils` (bank balances,
   etc.), the way a Cosmos-side observer would see it.
5. **Load ABIs via `precompileUtils`** (`precompileContract` / `precompileInterface`),
   never hand-written fragments, so specs track `precompiles/<name>/abi.json`.
6. **Negative tests may go through `rawSei`** to bypass ethers' client-side
   validation when the *node's* behavior is the subject.
