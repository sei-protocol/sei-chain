# rpc_tests

Self-contained module for verifying Sei's EVM JSON-RPC against a real local
**geth** reference node. Spec files in here intentionally do **not** import from
`shared/User`, `shared/Deployer`, or any other top-level utility — everything
the suite needs (utilities, contracts, tooling) lives under
`integration_test/rpc_tests/`, and the module has its own `package.json`,
`tsconfig.json`, and Hardhat compile config so it installs and runs in isolation.

## Install (one-time)

```bash
cd integration_test/rpc_tests
npm install        # installs ethers v6, cosmjs, @sei-js/cosmos, mocha + tsx, hardhat
npm run compile    # compiles ./contracts -> ./artifacts (TestERC20, RealGasBurner, SimpleAccount7702)
```

## What this suite proves

For every JSON-RPC method we care about, the spec file in `eth/` (and future
namespace dirs like `debug/`, `sei/`, etc.) answers one or more of:

- **Happy path.** The method returns the expected value/shape for valid input.
- **Schema parity.** The response shape on Sei matches geth for the same call.
- **Empty / null handling.** Absent data is represented correctly (`[]`, `null`,
  `0x`, etc.) and never as the wrong empty form.
- **Wrong params / error handling.** Bad input yields the correct JSON-RPC error
  code and message — asserted **byte-for-byte against geth**.

### Reference clients

- **geth `--dev` (primary).** Sei vendors go-ethereum's RPC layer, so a local
  `geth --dev` node reproduces geth's *exact* response and error envelopes
  (e.g. `-32602 "non-array args"`). We replicate the same deploy/tx scenario on
  both geth and Sei, then diff responses for the same logical operation. This is
  true apples-to-apples parity for schema, errors, and execution. geth cannot
  fork mainnet, so it runs an empty dev chain we drive with our own contracts.
- **anvil/Hardhat mainnet fork (optional, manual).** `npm run rpc:fork` starts
  one for ad-hoc checks that Sei's response *shape* holds up against messy
  real-world mainnet data. The automated suite does not assert against it — geth
  is the only reference — and it is **not** reliable for error envelopes
  (anvil/Hardhat reimplement the RPC layer and diverge from geth).

> **Filter & subscription coverage is deliberately Sei-only.** The log-filter and
> WebSocket specs (`eth_getLogs`, `eth_newFilter`, `eth_getFilterChanges`,
> `eth_newBlockFilter`, `eth_subscribe`) assert Sei's *functional* behavior and
> pin each result to Sei's own `eth_getLogs` oracle; they do **not** cross-check
> geth value-for-value. geth WS is not in CI, and polling-filter cursor semantics
> legitimately diverge between clients, so geth parity here is limited to error
> envelopes and handle shapes (e.g. `eth_newBlockFilter` id shape,
> `eth_newFilter` malformed-topic error). Do not expect symmetric geth asserts
> across every filter/subscription case.

## Layout

```
integration_test/rpc_tests/
├── package.json                # module deps + scripts (compile / rpc:* / test:rpc)
├── tsconfig.json               # TypeScript config for the module
├── hardhat.config.ts           # compile-only config: contracts/ -> artifacts/
├── contracts/                  # TestERC20.sol, GasBurner.sol, SimpleAccount7702.sol
├── .mocharc.bootstrap.json     # runs _start/ sequentially
├── .mocharc.run.json           # mocha config for the spec run (single process)
├── scripts/run-ci.sh           # orchestrator: deps + compile + geth + bootstrap + run + merge
├── config/endpoints.ts         # env-driven endpoints
├── utils/                      # grouped by domain
│   ├── constants.ts            # shared values: HD path, USEI/WEI_PER_USEI, staking addr, chain id
│   ├── format.ts               # regex matchers: HEX_QUANTITY / ADDRESS / HASH32 / BLOOM256 / …
│   ├── chainUtils.ts           # providers + raw JSON-RPC + error parity + waitFor + EIP-1559 fee math
│   ├── evmUtils.ts             # EvmAccount + funding + contract deploy + EIP-7702 auth
│   ├── cosmosUtils.ts          # bank query/send + admin funding/association + fee_collector
│   ├── testUtils.ts            # runtime state + claimPool + expectSameError + ERC20 calldata
│   └── txUtils.ts              # block/tx fixtures + block/receipt/count/raw-tx assertions
├── hardhat/                    # standalone fork config (chainId 1)
├── runtime/                    # gitignored, holds runtime.json
├── _start/
│   └── 00_bootstrap.spec.ts    # one-time setup
└── eth/                        # the actual specs (one dir per RPC namespace)
```

New RPC namespaces just need their own directory of `*.spec.ts` files (e.g.
`debug/`, `sei/`, `txpool/`); the runner picks up any `*/*.spec.ts` automatically.

## Runner (recommended)

```bash
cd integration_test/rpc_tests
npm install && npm run compile     # one-time
# start your local Sei devnet first (e.g. `make docker-cluster-start` from the repo root)
npm run rpc:ci                     # == bash scripts/run-ci.sh
```

`scripts/run-ci.sh` is the single orchestrator, used both locally and by the
`EVM RPC Parity (geth reference)` matrix entry in
`.github/workflows/integration-test.yml`. It assumes a Sei EVM RPC is already
reachable (the workflow boots the 4-node cluster; locally you start it yourself),
then end to end:

1. Installs deps (`npm ci`; skip with `SKIP_NPM_CI=true`) and compiles contracts.
2. Waits for the Sei EVM RPC on `:8545` and for the chain to be producing blocks.
3. Installs (on Linux, via the Ethereum PPA when `geth` is absent) and starts the
   geth `--dev` reference node (`npm run rpc:geth`), waiting for `:9547`.
4. Runs `rpc:bootstrap` then `rpc:run` in a single mocha process.
5. Merges the per-phase mochawesome JSON into one combined HTML report at
   `reports/merged/rpc-tests.html`.

The geth node is always killed on exit and the script exits non-zero on any
failure. Knobs: `SEI_EVM_RPC`, `RPC_ETH_GETH`, `SEI_TIMEOUT`, `GETH_TIMEOUT`,
`SKIP_NPM_CI`.

## Reporting

Each phase writes a mochawesome JSON (`reports/new_rpc/bootstrap.json`,
`reports/new_rpc/run.json`). `npm run report:merge` combines them via
`mochawesome-merge` + `mochawesome-report-generator` into a single interactive
report at `reports/merged/rpc-tests.html` (`run-ci.sh` does this for you).

## Running manually

All commands run from `integration_test/rpc_tests/`.

```bash
# 1. In a dedicated terminal, start the geth reference node. Leave it up.
npm run rpc:geth      # geth --dev on http://127.0.0.1:9547  (requires geth on PATH)

# 2. Make sure a local Sei node is up on http://localhost:8545 (the project's
#    usual local devnet, e.g. `make docker-cluster-start` from the repo root).

# 3. (Optional) start the anvil/Hardhat mainnet fork for data-shape sanity checks.
npm run rpc:fork      # http://127.0.0.1:9546

# 4. Run the suite (single mocha process).
npm run test:rpc      # bootstrap + run, recommended
# or, piecewise:
npm run rpc:bootstrap # writes runtime/runtime.json
npm run rpc:run       # runs every eth/*.spec.ts via .mocharc.run.json
```

> **Why a single process.** Every spec shares the one Sei chain and the
> bootstrap's funded-account pool, so a parallel run would make specs contend on
> the base fee and reuse pool keys (`claimPool` hands out disjoint slices via a
> module-level cursor, which is only correct in-process). The suite therefore runs
> serially; `report:merge` globs `bootstrap.json` + `run.json` into one report.

Individual files can be run with `mocha` (which picks up `tsx` via `.mocharc`):

```bash
npx mocha --require tsx eth/eth_blockNumber.spec.ts
```

…but only after `npm run rpc:bootstrap` has produced `runtime/runtime.json`.

## Configuration

| Variable                | Default                                            |
| ----------------------- | -------------------------------------------------- |
| `SEI_EVM_RPC`           | `http://localhost:8545`                            |
| `SEI_COSMOS_RPC`        | `http://localhost:26657`                           |
| `SEI_REST`              | `http://localhost:1317`                            |
| `RPC_ETH_GETH`          | `http://127.0.0.1:9547` (geth --dev, primary)      |
| `RPC_ETH_FORK`          | `http://127.0.0.1:9546` (anvil/Hardhat, optional)  |
| `ETH_MAINNET_UPSTREAM`  | required for `npm run rpc:fork` (no default — bring your own mainnet RPC URL) |
| `ETH_MAINNET_FORK_BLOCK`| unset (latest)                                     |
| `SEI_ADMIN_MNEMONIC`    | local devnet admin (in `endpoints.ts`)             |
| `RPC_POLLING_INTERVAL_MS`| `100` (Sei blocks are ~400ms; ethers default 4s is too slow) |

## Authoring a new spec

Structure every spec into the four sections (happy path / schema matching /
empty-null / wrong params), e.g.:

```ts
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { HEX_QUANTITY } from '../utils/format';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

describe('eth_getBalance', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();
    let runtime: RuntimeState;

    before(() => {
        runtime = readRuntimeState();
    });

    describe('happy path', () => {
        it('returns a canonical hex quantity', async () => {
            const bal = await sei.send('eth_getBalance', [runtime.funded.admin, 'latest']);
            expect(bal).to.match(HEX_QUANTITY);
        });
    });

    describe('wrong params / error handling', () => {
        it('rejects a missing block tag identically to geth', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBalance', [runtime.funded.admin]),
                rawGeth('eth_getBalance', [runtime.funded.admin]),
            ]);
            // assert Sei matches geth's exact code + message
            expect(s.error?.code).to.equal(g.error?.code);
        });
    });
});
```

Rules of the road for new specs:

1. **Read-only at runtime.** Bootstrap is the only writer of `runtime.json`. If
   you need new pre-computed state, add it to the `RuntimeState` interface and
   populate it in `_start/00_bootstrap.spec.ts`.
2. **Pool accounts are single-use.** Claim fresh accounts with
   `claimPool(runtime, provider, count, label)` (testUtils), which hands out a
   disjoint slice of `runtime.funded.pool` on every call; never reuse a pool key
   across specs.
3. **No imports from `shared/`** — keep this module self-contained.
4. **Negative tests go through `rawSei` / `rawGeth`** to bypass ethers'
   client-side validation, so we assert the *node's* behavior, not ethers'.
5. **geth is the error/schema source of truth.** Assert Sei matches `rawGeth`
   exactly for shared methods. Sei-only methods (`sei_*`) have no reference — just
   assert the Sei behavior.