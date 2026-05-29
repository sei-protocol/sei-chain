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

For every JSON-RPC method we care about, the spec file in `eth/`, , `debug/`, etc. answers one or more of:

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
- **anvil/Hardhat mainnet fork (optional secondary).** Only a sanity check that
  Sei's response *shape* holds up against messy real-world mainnet data. It is
  **not** a reliable reference for error envelopes — anvil/Hardhat reimplement
  the RPC layer (Rust) and diverge from geth. Tests must never assert exact
  error parity against the fork.

## Layout

```
integration_test/rpc_tests/
├── package.json                # module deps + scripts (compile / rpc:* / test:rpc)
├── tsconfig.json               # TypeScript config for the module
├── hardhat.config.ts           # compile-only config: contracts/ -> artifacts/
├── contracts/                  # TestERC20.sol, GasBurner.sol, SimpleAccount7702.sol
├── .mocharc.bootstrap.json     # runs _start/ sequentially
├── scripts/run-parallel.sh     # shards specs into N mocha processes (parallel run)
├── .mocharc.run.json           # single-process fallback config
├── config/endpoints.ts         # env-driven endpoints
├── utils/
│   ├── providers.ts            # seiRpc() / gethRpc() / forkRpc() / bothProviders()
│   ├── rpc.ts                  # rawJsonRpc + rawSei/rawGeth + captureRpcError + expectJsonRpcError
│   ├── format.ts               # HEX_QUANTITY / ADDRESS / HEX_DATA matchers
│   ├── wallet.ts               # EvmAccount (mnemonic / privkey / random)
│   ├── funding.ts              # fundEvm / fundManyEvm
│   ├── deploy.ts               # deployContract / deployTestErc20 / abiOf
│   ├── state.ts                # read/write runtime/runtime.json
│   └── waitFor.ts              # sleep + waitUntil
├── hardhat/                    # standalone fork config (chainId 1)
├── runtime/                    # gitignored, holds runtime.json
├── _start/
│   └── 00_bootstrap.spec.ts    # one-time setup
└── eth/ sei/ sei2/ debug/ ...  # the actual specs
```

## One-shot runner (recommended)

```bash
cd integration_test/rpc_tests
npm install && npm run compile     # one-time
npm run test:rpc:full
```

`test:rpc:full` (see `scripts/run-full.sh`) does everything end to end:

1. `DOCKER_DETACH=true make docker-cluster-start` at the repo root and waits for the
   4-node cluster (`build/generated/launch.complete`) **and** the EVM RPC on `:8545`.
2. Starts the geth `--dev` reference node (`npm run rpc:geth`) and waits for `:9547`.
3. Runs the suite (`rpc:bootstrap` then `rpc:run`) — it does **not** abort on test
   failures, so you always get a report.
4. Merges the per-phase mochawesome JSON into one combined HTML report at
   `reports/merged/rpc-tests.html`.

The geth node it starts is always killed on exit. The docker cluster is left up by
default (re-running is still safe — `docker-cluster-start` stops any prior cluster
first); set `STOP_CLUSTER=true` to tear it down too. Other knobs: `CLUSTER_TIMEOUT`,
`GETH_TIMEOUT`, `SEI_TIMEOUT`.

## Reporting

Each phase writes a mochawesome JSON (`reports/new_rpc/bootstrap.json`,
`reports/new_rpc/run.json`). `npm run report:merge` combines them via
`mochawesome-merge` + `mochawesome-report-generator` into a single interactive
report at `reports/merged/rpc-tests.html` (the one-shot runner does this for you).

## Running manually

All commands run from `integration_test/rpc_tests/`.

```bash
# 1. In a dedicated terminal, start the geth reference node. Leave it up.
npm run rpc:geth      # geth --dev on http://127.0.0.1:9547  (requires geth on PATH)

# 2. Make sure a local Sei node is up on http://localhost:8545 (the project's
#    usual local devnet, e.g. `make docker-cluster-start` from the repo root).

# 3. (Optional) start the anvil/Hardhat mainnet fork for data-shape sanity checks.
npm run rpc:fork      # http://127.0.0.1:9546

# 4. Run the suite.
npm run test:rpc      # bootstrap + parallel run, recommended
# or, piecewise:
npm run rpc:bootstrap # writes runtime/runtime.json
npm run rpc:run       # parallel run (process-sharded), requires runtime.json
npm run rpc:run:serial # single-process fallback via .mocharc.run.json
```

> **How parallelism + reporting coexist.** mocha's own `--parallel` mode is
> incompatible with mochawesome — its single main-process reporter can't
> consolidate worker results and writes a corrupt `results: [false]`, dropping the
> rpc specs from the merged report. So `rpc:run` (`scripts/run-parallel.sh`) shards
> the spec files into `RPC_JOBS` buckets (default 8) and runs one mocha **process**
> per bucket concurrently. Each process writes its own well-formed shard
> (`reports/new_rpc/run-<n>.json`); `report:merge` globs them with `bootstrap.json`
> into a single combined report. Tune concurrency with `RPC_JOBS`.

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
| `ETH_MAINNET_UPSTREAM`  | Alchemy mainnet URL (used only by `yarn rpc:fork`) |
| `ETH_MAINNET_FORK_BLOCK`| unset (latest)                                     |
| `SEI_ADMIN_MNEMONIC`    | local devnet admin (in `endpoints.ts`)             |
| `RPC_POLLING_INTERVAL_MS`| `100` (Sei blocks are ~400ms; ethers default 4s is too slow) |

## Authoring a new spec

Structure every spec into the four sections (happy path / schema matching /
empty-null / wrong params), e.g.:

```ts
import { expect } from 'chai';
import { bothProviders } from '../utils/providers';
import { rawSei, rawGeth, expectJsonRpcError } from '../utils/rpc';
import { HEX_QUANTITY } from '../utils/format';
import { readRuntimeState, RuntimeState } from '../utils/state';

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
2. **Pool accounts are single-use.** Each parallel worker that needs a fresh
   account should claim a different `runtime.funded.pool[i]` — usually by
   hashing its spec file name or by index.
3. **No imports from `shared/`** — keep this module self-contained.
4. **Negative tests go through `rawSei` / `rawGeth`** to bypass ethers'
   client-side validation, so we assert the *node's* behavior, not ethers'.
5. **geth is the error/schema source of truth.** Assert Sei matches `rawGeth`
   exactly for shared methods. The anvil fork (`rawFork`) is only for real-data
   shape sanity checks, never exact error parity. Sei-only methods (`sei_*`)
   have no reference — just assert the Sei behavior.

## Pending migration

Empty placeholder spec files (`*.spec.ts` with no content) under `debug/`,
`echo/`, `net/`, and `web3/` are stubs waiting to be filled in. They are safe to
run (mocha just registers nothing) but assert nothing yet.
