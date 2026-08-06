# Pacific replay load generator

Standalone TypeScript package for capturing canonical Pacific-1 blocks and replaying equivalent, newly signed traffic on Arctic-1 or Atlantic-2. It never rebroadcasts Pacific signatures or assumes Pacific addresses and state exist on the target.

## Safety

- Capture and validation are read-only.
- Deployment, provisioning, and replay are dry-run unless `EXECUTE=1`.
- Executed replays verify EVM and Cosmos chain IDs, user/deployment manifests, and deployed bytecode before submission.
- Wrapped EVM transactions are correlated by reconstructing their signed hash from `MsgEVMTransaction`; Cosmos and EVM indexes are never assumed to align.
- Privileged module traffic becomes labelled bank-shaped load by default (`PRIVILEGED_REPLAY_MODE=skip` omits it).
- Successful unknown contract creations use `SyntheticCreationHarness` for bounded safe CREATE/CREATE2 load. Unknown traced calls use the allowlisted `CallGraphHarness`; untraced calls use `ProfileLoadHarness`. None executes source-selected target addresses or untrusted Pacific initcode.
- Mnemonics come only from `TARGET_MNEMONIC` or `SEI_ADMIN_MNEMONIC` and are never persisted.

## Setup

```bash
cd integration_test/load_generator
npm install
npm run compile
```

All configuration is parsed in `src/config.ts`. Copy the tracked template to the ignored `.env` file and set target endpoints and the mnemonic there:

```bash
cp .env.example .env
$EDITOR .env
```

Every command loads `.env` automatically. To use a different file, set `DOTENV_CONFIG_PATH`, for example `DOTENV_CONFIG_PATH=.env.arctic npm run replay:deploy`. `.env` is gitignored; never put a real mnemonic in `.env.example` or another tracked file. Explicit shell environment variables override values loaded from the file.

At minimum, confirm these values before an executed run:

```bash
TARGET_NETWORK=arctic-1
TARGET_MNEMONIC="<funded admin mnemonic>"
EXECUTE=1
RUN_DURATION_HOURS=2
BUFFER_START_MODE=latest
LOAD_DEPLOYMENT=runtime/replay-deployments/arctic-1-v4.json
```

## Single-command run

After configuring `.env`, the recommended one-command continuous run is:

```bash
npm run replay:start:buffered
```

This command compiles contracts, verifies or deploys schema-v4 fixtures, idempotently associates and funds users, captures the latest safe Pacific window, starts the replay follower, continuously appends new blocks, emits metrics/audits, and cleans consumed segment files. It runs for `RUN_DURATION_HOURS` from `.env` and mutates the target only when `EXECUTE=1`.

For a finite capture followed by one bounded replay instead:

```bash
npm run replay:start
```

A valid schema-v4 deployment is reused after its chain ID, bytecode hashes, SushiSwap provenance, creation harness, and protocol wiring are checked. Only `FORCE_DEPLOY=1` replaces it. Existing users are only topped up to `FUND_SEI`. Buffered mode starts from a fresh latest window by default; use `BUFFER_START_MODE=resume` only when intentionally continuing a live, non-pruned corpus.

## Capture and validate

Capture the latest safe 20-minute window in atomic 200-block segments:

```bash
REPLAY_DIR=runtime/replay/pacific-1/pacific-1-20m npm run replay:capture
npm run replay:validate
```

Controls include `RECORD_MINUTES`, `SEGMENT_BLOCKS`, `TIP_LAG_BLOCKS`, `START_BLOCK`, `END_BLOCK`, `MAINNET_RPC`, `COSMOS_RPC`, `RPC_CONCURRENCY`, `COSMOS_RPC_CONCURRENCY`, and `BLOCKS_PER_BATCH`. Fixed ranges resume from `capture-checkpoint.json`.

Trace capture is controlled by `TRACE_CAPTURE_MODE`:

- `off`: no debug trace calls. Replay still has transactions, receipts, calldata, deployed runtime size/hash, and top-level gas, but unknown calls use the coarse profile fallback.
- `calls` (default): requests `callTracer`. This preserves bounded call topology, call type, depth, per-frame gas and CREATE versus CREATE2, but not accurate opcode or storage-operation counts.
- `full`: includes `calls`, memory/stack/storage-disabled struct logs, and `prestateTracer` diff mode. This adds aggregate SLOAD, SSTORE, log, hash, create and changed-storage pressure used by the harnesses.

Additional controls are `TRACE_CONCURRENCY` (default 1), `TRACE_MAX_DEPTH` (8), `TRACE_MAX_FRAMES` (64), `TRACE_TIMEOUT_MS` (30000), and `TRACE_MAX_RETRIES` (3). Native transfers are not traced. Trace failures are retained as per-transaction availability/error metadata and do not fail the segment.

Tracing is substantially more expensive for the Pacific RPC than ordinary block capture; keep concurrency low and use `full` only when operation/state pressure is needed. Segment files persist only normalized call frames, bounded opcode counts, and state-diff counts. Raw struct logs, stack, memory, storage values, output, and per-frame calldata are never written.

Each segment retains full Cosmos transaction bytes, decoded protobuf payloads, EVM transactions and receipts, top-level calldata, source byte estimates, code hashes, timestamps, and EVM/CometBFT continuity hashes. Successful contract creations also record the deployed runtime code size/hash and CREATE/CREATE2 classification. Captures retain full unlinked EVM transaction objects so they can be replayed exactly once. Hash-only legacy corpora must be recaptured because their omitted transactions cannot be reconstructed offline.

## Deploy fixtures

The package deploys canonical production SushiSwap V2 factory/router bytecode, WETH9, and a factory-created SushiSwap pair, plus permissionless `TestERC20` tokenA/tokenB, a replay NFT, `ProfileLoadHarness`, `CallGraphHarness`, and `SyntheticCreationHarness`. The factory and router come from official Sushi mainnet deployment artifacts at commit `94ea7712daaa13155dfab9786aacf69e24390147`; the pair init-code hash is the production value `0xe18a34eb0e04b04f7a0ac29a6e80748dca96319b42c54d679cb821dca90c6303`. `DeterministicMasterChef` remains a separate replay fixture and is not represented as official Sushi MasterChef. The fixture graph also includes one shared proxy-token implementation and separate receipt/reward/output proxies; a callback V3 pool/router; deterministic farming; oracle/rate/comptroller-backed lending; exchange-rate-backed liquid staking; and a proxy strategy vault with a nested delegatecall strategy module and adapter. Implementations, proxies, helpers, and protocol contracts are code-hashed and all critical wiring is checked before the manifest is written.

```bash
TARGET_NETWORK=arctic-1 EXECUTE=1 npm run replay:deploy
```

Existing schema-v4 manifests are verified and reused. Set `FORCE_DEPLOY=1` to replace one. Defaults are `runtime/replay-deployments/<network>-v4.json`; older manifests are intentionally not reused. Canonical source, artifact checksums, compiler settings, and deployment provenance are recorded in `vendor/sushiswap-v2/PROVENANCE.json`.

The vendored SushiSwap Solidity source and executable artifacts are GPL-3.0-covered third-party material. See `THIRD_PARTY_NOTICES.md` and the retained `contracts/uniswapv2/LICENSE`; the surrounding deterministic fixture implementations remain separate.

## Provision users

```bash
TARGET_NETWORK=arctic-1 USER_COUNT=100 FUND_SEI=100 EXECUTE=1 npm run replay:users
```

Users are deterministically derived from the target mnemonic, associated, funded, and recorded without private keys in `runtime/replay-users/<network>-100.json`.

## Inspect or execute

Dry-run classification requires no funded users or deployed fixtures:

```bash
TARGET_NETWORK=arctic-1 npm run replay:run
```

Execute a bounded segment at low speed:

```bash
TARGET_NETWORK=arctic-1 MAX_SEGMENTS=1 TIME_SCALE=0.1 EXECUTE=1 npm run replay:run
```

Important bounds are `MAX_TPS`, `WORKER_COUNT`, `MAX_PENDING_PER_LANE`, `MAX_GAS_PER_TX`, `FIXTURE_PREPARE_GAS_LIMIT`, `MAX_CALLDATA_BYTES`, `MAX_COSMOS_BYTES`, `MAX_COSMOS_MESSAGES`, and `MAX_VALUE_WEI`. `TIME_SCALE=1` preserves source-relative timing. Completed source blocks are checkpointed separately per target.

`npm run replay:run` resumes from that target checkpoint. Set `REPLAY_FROM_START=1` only when intentionally replaying the selected corpus again from its first block; the new run then replaces the checkpoint as blocks complete.

## Transaction typology

EVM envelopes retain legacy type 0 and access-list type 1, use dynamic-fee type 2 for ordinary modern traffic, and rebuild type 4 with a new target EIP-7702 authorization. Type 3 blob transactions are skipped.

Semantic EVM translations are:

- Native transfer → worker-to-worker native SEI.
- ERC20 `transfer`, `approve`, and `mint` → fixture tokenA/tokenB.
- `swapExactTokensForTokens` → canonical SushiSwap V2 router.
- `exactInputSingle` → callback V3 fixture router.
- MasterChef `deposit` and `withdraw` → `DeterministicMasterChef`.
- Aave-shaped `supply`/`withdraw` and cToken-shaped `mint`/`redeem`/`borrow`/`repayBorrow` → lending proxy fixture.
- Liquid `stake`, ERC4626-shaped `deposit`, and `requestWithdrawal` → liquid-staking proxy fixture.
- ERC4626 `deposit`/`withdraw`/`redeem` → strategy-vault proxy fixture.
- ERC721 `safeMint` → replay NFT.

The shared ERC4626 deposit selector is routed using captured recipient code hash when possible and a deterministic fallback otherwise. These protocol fixtures execute real storage updates, transfers, callbacks and delegatecalls, but only SushiSwap V2 uses canonical production protocol contracts.

Non-semantic EVM translations are:

- Successful unknown contract creation → `SyntheticCreationHarness` with fidelity `creation-shape`. It performs a real bounded CREATE or CREATE2 using safe generated initcode, matching captured initcode size, deployed runtime size, constructor gas and bounded store pressure. The deployed runtime is STOP-filled; original Pacific initcode is never executed.
- Unknown call with a usable trace → `CallGraphHarness` with fidelity `trace-shape`.
- Unknown call without a usable trace → `ProfileLoadHarness` with fidelity `shape`.
- Failed creation, unsupported blob traffic, or missing required harness → shape fallback or explicit skip.

Cosmos translations are:

- Pure bank `MsgSend` transactions → bounded worker-to-worker `MsgSend` operations.
- Wrapped EVM messages → skipped in the Cosmos lane and handled by the EVM lane.
- Oracle, governance, slashing and upgrade traffic → skipped with `PRIVILEGED_REPLAY_MODE=skip`, or replaced by labelled bank-shaped traffic with `shape`.
- Other unsupported messages → bank-shaped traffic preserving bounded message count, fee/gas and transaction-byte pressure.

Protocol arguments are bounded synthetic amounts, not decoded claims of exact source state. Natural ABI calldata is padded up to the bounded source size to preserve byte pressure. Startup preparation permissionlessly mints tokenA/tokenB, installs worker approvals, and creates small farm, lending, staking, and vault positions; expect roughly 10–15 one-time transactions per new worker, fewer on subsequent runs. Workers never receive proxy-token mint rights. Position top-ups are view-idempotent except staking/vault share-price drift, where setup uses a documented bounded approximation.

Unknown traced calls use actual bounded `CALL`, `STATICCALL`, and `DELEGATECALL` operations over the captured preorder topology, plus bounded read/write/hash/log/gas pressure. Successful top-level creations now deploy a bounded synthetic contract; nested CREATE/CREATE2 frames inside unknown calls remain represented as operation pressure. Unknown calls without usable traces preserve bounded selector/calldata/gas shape through `ProfileLoadHarness`.

The production-shaped fixtures reproduce corresponding protocol operations and nested call mechanisms, but not source liquidity, prices, balances, deadlines, slippage, rewards, interest, share prices, or revert semantics. The generic call graph reproduces structure and operation pressure only. Static contexts suppress synthetic writes/logs. Unlinked EVM transactions are replayed once from their stored full objects. Their order is merged using EVM indexes and linked wrapper anchors; Cosmos-only placement is interpolated in Cosmos order because the two runtimes do not expose one authoritative shared transaction index. Local Hardhat traces verify V3 `CALL`/`STATICCALL`/proxy `DELEGATECALL` and vault proxy/nested-strategy `DELEGATECALL`; target-chain traces remain environment-dependent.

## Buffered continuous mode

If schema-v4 fixtures and funded users already exist, start replay without deployment or provisioning:

```bash
TARGET_NETWORK=arctic-1 RUN_DURATION_HOURS=2 EXECUTE=1 npm run replay:buffered
```

For a complete first run, including compile, deployment and user provisioning:

```bash
TARGET_NETWORK=arctic-1 EXECUTE=1 npm run replay:start:buffered
```

Capture richer storage and opcode pressure before a bounded replay:

```bash
TRACE_CAPTURE_MODE=full npm run replay:capture
npm run replay:validate
TARGET_NETWORK=arctic-1 EXECUTE=1 npm run replay:run
```

By default (`BUFFER_START_MODE=latest`), the supervisor archives any previous artifacts, removes their captured segment files, captures the latest `INITIAL_BUFFER_BLOCKS` safe blocks (200 by default), and then appends every newly available safe range up to `SEGMENT_BLOCKS` blocks. The persistent replay follower consumes those ranges as they arrive. After a block checkpoint is durable, buffered mode periodically removes fully consumed segments while retaining the newest completed segment as the collector's continuity anchor. Audit logs, reports, manifests, and checkpoints are retained. Configure this with `CLEANUP_CONSUMED_SEGMENTS` (default `1` in buffered mode), `RETAIN_COMPLETED_SEGMENTS` (default `1`), and `CLEANUP_INTERVAL_BLOCKS` (default `200`). Direct `replay:run` leaves segments untouched unless cleanup is explicitly enabled. Use `BUFFER_START_MODE=resume` only when intentionally continuing an existing, non-pruned corpus; resume mode retains the `MIN_BUFFER_MINUTES` / `RESUME_BUFFER_MINUTES` hysteresis. `TIME_SCALE>1` is rejected unless `ALLOW_BUFFER_DRAIN=1`.

Run length is configured with `RUN_DURATION_HOURS` (default `2` for buffered mode). `RUN_DURATION_SECONDS` remains available for short tests and takes precedence when both variables are set.

## Metrics, audits, and observability

Executed replay exposes Prometheus metrics on `0.0.0.0:9465/metrics` and `/healthz`; set `METRICS_PORT=0` to disable.

```bash
npm run dashboard:up
```

Grafana is at `http://localhost:3000/d/pacific-replay`; Prometheus is at `http://localhost:9090`. Prometheus scrapes the host runner at `host.docker.internal:9465`, so keep `METRICS_HOST=0.0.0.0` and `METRICS_PORT=9465` when using the bundled stack. The dashboard shows scrape/process health, throughput, successful and failed inclusion, latency, pending work, adapter/fidelity mix, source-vs-target bytes and gas, calldata-size fidelity, trace availability and operation pressure, skips, and source cursors. The defaults are `admin` / `replay`; override them with `GRAFANA_ADMIN_USER` and `GRAFANA_ADMIN_PASSWORD`. These credentials and the stack are for local development only. Stop the stack with `npm run dashboard:down`.

Each executed run writes:

- `bucket-audit-<target>-<run>.jsonl`: every offered transaction, source identifiers, adapter/fidelity, trace availability and bounded frame/delegate/read/write/state-diff counts, target hash, sizes, and outcome.
- `unbucketed-<target>-<run>.jsonl`: shape or skipped traffic without a semantic adapter.
- `replay-report-<target>-<run>.json`: aggregate adapter, fidelity, byte, inclusion, error, and skip metrics.

Override paths with `BUCKET_AUDIT_PATH`, `UNBUCKETED_AUDIT_PATH`, and `REPLAY_REPORT`.

## Verification

```bash
npm run compile
npm run typecheck
npm test
npm run test:fixtures
```
