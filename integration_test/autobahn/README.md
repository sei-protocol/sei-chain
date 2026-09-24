# Autobahn EVM-only E2E clusters

`autobahn-e2e` manages the four-validator, disk-backed EVM-only Autobahn
topology used for integration and load testing. It can run the topology in
local Docker or on one AWS EC2 instance.

Run every command in this document from the root of a `sei-chain` checkout.
The examples use the default cluster name, `autobahn-evmonly`. If `--name` is
passed to `deploy`, pass the same name to `list`, `forward`, and `teardown`.

## Prerequisites

Both targets require Go 1.25.6 and `make`. Local deployment also requires a
running Docker engine with Docker Compose v2. AWS deployment requires the AWS
CLI, `git`, and `ssh`, plus credentials allowed to manage EC2 instances,
security groups, and key pairs. The inspection, balance, and receipt examples
also use `jq` and Foundry's `cast`.

Build the manager once:

```sh
make build-autobahn-e2e
```

This creates `./autobahn-e2e` outside `build/`. The node image build runs
`make clean`, so placing the manager under `build/` would remove it during
deployment.

Cluster metadata is stored under `~/.sei/autobahn-e2e` by default. Use
`--state-dir` or `AUTOBAHN_E2E_STATE_DIR` to isolate it. The metadata contains
resource identifiers and, for an AWS cluster using a managed key, its private
SSH key.

## Start a local cluster

The deploy command builds the Linux node image, initializes all four nodes,
starts Docker Compose in the background, and waits for every EVM JSON-RPC
listener to become ready:

```sh
./autobahn-e2e deploy --target local
./autobahn-e2e list --name autobahn-evmonly
```

A ready cluster looks like this:

```text
CLUSTER            TARGET  NODE    STATUS   HEIGHT  EVM TARGET
autobahn-evmonly   local   node-0  running  0       127.0.0.1:8545
autobahn-evmonly   local   node-1  running  0       127.0.0.1:8547
autobahn-evmonly   local   node-2  running  0       127.0.0.1:8549
autobahn-evmonly   local   node-3  running  0       127.0.0.1:8551
```

Height remains zero until a transaction is submitted because empty blocks are
disabled. Docker already publishes every local node, so `forward` is normally
unnecessary. To relay a node to a different local port, keep this blocking
command open in another terminal:

```sh
./autobahn-e2e forward \
  --name autobahn-evmonly \
  --node node-2 \
  --local-port 18545
```

Only one local Sei Docker topology can run at a time. Deployment refuses to
replace existing `sei-node-*` containers or existing manager metadata.

## Start a cluster on AWS

The AWS target creates one Ubuntu EC2 host and runs the same four-node Docker
topology on it. Only SSH is opened in the managed security group. EVM JSON-RPC
stays private and is accessed through `forward`.

```sh
./autobahn-e2e deploy --target aws \
  --name my-autobahn \
  --region us-west-2

./autobahn-e2e list --name my-autobahn
```

In another terminal, forward one node to the load-generator host:

```sh
./autobahn-e2e forward \
  --name my-autobahn \
  --node node-0 \
  --local-port 18545
```

One forwarded endpoint is sufficient: validator EVM proxying is enabled by
default, so transactions submitted to node 0 are forwarded to the Autobahn
validator that owns the sender's shard. To distribute load across all four
entry points, start four `forward` processes with distinct local ports and put
all four URLs in the `sei-load` configuration.

AWS credentials use the AWS CLI credential chain. Use `--profile NAME` to
select a profile. If no credentials work in an interactive terminal, the
manager runs `aws configure`; credentials are never copied into cluster state.

By default, deployment creates an EC2 key pair and saves the private key in the
manager state directory with mode `0600`. To use an existing key pair instead:

```sh
./autobahn-e2e deploy --target aws \
  --name my-autobahn \
  --region us-west-2 \
  --key-name my-key-pair \
  --ssh-key ~/.ssh/my-key-pair.pem
```

The default security-group rule admits SSH only from the public IP detected at
deployment time. Use `--ssh-cidr` when a VPN, NAT, or IPv6 setup makes that
incorrect. Use `--subnet-id` if the region has no default VPC or the instance
needs a specific public subnet.

The default instance is `c7g.2xlarge` with 100 GiB of gp3 storage and the
current Ubuntu 24.04 ARM64 AMI from AWS Systems Manager. When changing
architecture, override `--instance-type` and `--ami-id` together.
`--repo-url` and `--ref` select the source built remotely; they default to this
checkout's origin and current commit. The selected commit must be reachable
from the EC2 host, so uncommitted local changes are not deployed.

## What deployment initializes

Initialization is part of `deploy`; no separate genesis command is needed.
The generated files are under `build/generated` locally and in the remote
checkout on AWS.

The operational EVM-only configuration is:

| Setting | Value |
| --- | --- |
| Autobahn validators | 4 |
| Validator voting power used by the EVM-only app | 1 each |
| EVM chain ID | `713715` |
| Initial EVM nonce | `0` |
| Initial balance of an address absent from FlatKV | `2^200` wei |
| Consensus block gas limit | `35,000,000` |
| Autobahn transaction limit | `2,000` transactions per block |
| Block interval | `400ms` |
| Empty blocks | disabled |
| Persistent state directory | `data/autobahn` under each node home |
| BlockDB minimum retention age | `30s` |

The node binary is `seid`. Each node first runs the normal `seid init` and
genesis scripts, then deployment enables `evm-only = true` in `config.toml` and
starts the node with:

```sh
seid start --chain-id sei --inv-check-period 0 --freeze-height 0
```

The shared genesis document contains four gentxs and four validators with raw
genesis power 10. On startup, `seid` replaces the Cosmos application with the
EVM-only application and derives its active four-validator set from
`autobahn.json`, assigning unit power to every committee member. Cosmos auth,
bank, staking, mint, and test-account state in `genesis.json` is therefore not
the EVM execution genesis.

The EVM executor does not pre-enumerate funded accounts. A previously unseen
20-byte EVM address reads as nonce 0 with `2^200` wei; its state is written to
FlatKV when it first changes. This test-only rule is why arbitrary accounts
generated by `sei-load` work without a funding transaction or root key.

Every validator opens the production-shaped Giga storage manager: FlatKV for
EVM state, littidx for receipts, and littblock for blocks. GigaSS is disabled
because EVM-only execution does not use it.

Inspect the generated local inputs with:

```sh
jq '{
  initial_height,
  chain_id,
  max_gas: .consensus_params.block.max_gas,
  validators: (.validators | length),
  gentxs: (.app_state.genutil.gen_txs | length)
}' build/generated/genesis.json

docker exec sei-node-0 jq '{
  validators: (.validators | length),
  max_txs_per_block,
  max_txs_per_second,
  allow_empty_blocks,
  block_interval,
  view_timeout,
  persistent_state_dir,
  block_db
}' /root/.sei/config/autobahn.json
```

## Run `sei-load`

Install the version used by this checkout into the ignored `build/tools`
directory:

```sh
mkdir -p build/tools
GOBIN="$PWD/build/tools" go install github.com/sei-protocol/sei-load@v0.0.1
```

The checked-in [`sei-load.local.json`](sei-load.local.json) is a ready local
four-endpoint configuration. For AWS with the single tunnel shown above, copy
it and change `endpoints` to only `http://127.0.0.1:18545`.

Start load and press Ctrl-C to stop it cleanly:

```sh
./build/tools/sei-load \
  --config integration_test/autobahn/sei-load.local.json \
  --metricsListenAddr 127.0.0.1:19698
```

Port 19698 avoids the ports published by the local node containers. The load
generator prints submission throughput and latency every `statsInterval`; its
Prometheus metrics are available at `http://127.0.0.1:19698/metrics`.

### Tune the workload

The most useful settings are:

- `scenarios`: load type and relative weight. Scenario names are
  case-insensitive. Native scenarios include `EVMTransfer`,
  `EVMTransferFast`, and `EVMTransferNoop`. Contract scenarios include
  `ERC20`, `ERC20Noop`, `ERC20Conflict`, `ERC721`, `StorageRW`, and
  `Disperse`; each contract scenario submits a deployment and waits for its
  receipt before load begins.
- `accounts.count`: size of the reusable sender pool. More accounts reduce
  nonce serialization and sender-key contention. Keep `newAccountRate` at zero
  for a stable fixed pool.
- `settings.tps` or `--tps`: offered TPS across all workers and endpoints. Zero
  means unlimited and is useful for saturation tests.
- `settings.workers` or `--workers`: sender tasks per endpoint. Raise this when
  client-side signing or HTTP concurrency is the bottleneck.
- `settings.bufferSize` or `--buffer-size`: queue capacity per endpoint.
- `--nodes N`: use only the first N configured endpoints.
- `--arrival-model open_loop --tps N --max-in-flight M`: schedule a fixed
  arrival rate and count overload as drops rather than allowing sender
  backpressure to hide it. The default `closed_loop` model measures achieved
  submission rate.

For a mixed workload, replace the scenario list with weighted entries:

```json
"scenarios": [
  {"name": "EVMTransfer", "weight": 80},
  {"name": "ERC20", "weight": 20}
]
```

`sei-load` controls offered TPS, not a literal transactions-per-block value.
At the default 400 ms interval, use this starting estimate:

```text
offered TPS = desired transactions per block / 0.4
            = desired transactions per block * 2.5
```

For example, start with `--tps 2500` to target roughly 1,000 submitted
transactions per block. Actual occupancy depends on admission latency,
contention, execution speed, and both block limits:

```text
maximum transactions per block = min(2000, floor(35000000 / tx gas limit))
```

A 21,000-gas native transfer is therefore gas-capped at 1,666 transactions,
before the 2,000-transaction cap. Contract scenarios request different gas
limits and generally reach a lower gas-derived cap. The deployment manager
does not currently expose block interval, block gas, or
`max_txs_per_block` flags; changing those producer-side limits requires
changing the genesis/config generator and redeploying the cluster.

Do not enable `trackReceipts`, `trackBlocks`, or `trackUserLatency` with the
current EVM-only RPC. `sei-load` implements receipt tracking by subscribing to
new heads and fetching blocks, rather than polling individual receipts, and
those methods are not exposed yet.

## Interact with a running cluster

Inspect node health and the last executed height at any time:

```sh
./autobahn-e2e list --name autobahn-evmonly
./autobahn-e2e list --name autobahn-evmonly --json
```

For local debugging, follow a validator's `seid` log:

```sh
tail -f build/generated/logs/seid-0.log
```

The public EVM JSON-RPC surface intentionally contains only:

- `eth_sendRawTransaction`, used by `sei-load` and `cast publish`;
- `eth_getTransactionReceipt`, for finalized receipts;
- `eth_getTransactionByHash`, for a finalized transaction's decoded fields;
- `eth_getBalance`, for the current committed EVM balance;
- `eth_getTransactionCount`, for the current committed nonce;
- `eth_getCode`, for the current committed contract code at an address;
- `eth_blockNumber`, for the current committed block height;
- `eth_chainId`, for the configured EVM chain ID;
- `eth_call`, for a read-only message call against current committed state;
- `eth_estimateGas`, for the gas a message needs against current committed
  state;
- `eth_getBlockByNumber` and `eth_getBlockByHash`, for a finalized block, by
  height (including any height still within the node's retention window) or
  by hash.

All other `eth_*` methods currently return JSON-RPC method-not-found. A lookup
for a pending or unknown hash returns `null`.

### Fetch balances with `cast`

`eth_getBalance` accepts `latest`, `safe`, `finalized`, and `pending`; all four
read the current committed state because Sei has instant finality. Explicit
block numbers and hashes return an error because historical EVM-only state is
not wired yet.

Every previously unseen address starts with the test-only `2^200` wei balance:

```sh
cast balance \
  --rpc-url http://127.0.0.1:8545 \
  0x000000000000000000000000000000000000dEaD
```

### Fetch receipts with `cast`

`cast receipt` works for a known finalized transaction hash. Contract
scenarios print their deployment hash, which can be queried directly:

```sh
cast receipt \
  --rpc-url http://127.0.0.1:8545 \
  0xYOUR_TRANSACTION_HASH
```

Without `--async`, `cast receipt` polls until the receipt exists, now also
polling `eth_blockNumber` for confirmation counting. Add `--async` for a
one-shot lookup that fails immediately when the hash is not found.

For a repeatable end-to-end check, create a new throwaway key, sign completely
offline, publish the raw transaction, and fetch its receipt:

```sh
RPC_URL=http://127.0.0.1:8545
TEST_KEY=$(cast wallet new --json | jq -r '.[0].private_key')
RAW_TX=$(cast mktx \
  0x000000000000000000000000000000000000dEaD \
  --private-key "$TEST_KEY" \
  --legacy \
  --nonce 0 \
  --chain 713715 \
  --gas-limit 21000 \
  --gas-price 1gwei \
  --value 1)
TX_HASH=$(cast publish --async --rpc-url "$RPC_URL" "$RAW_TX")

echo "$TX_HASH"
sleep 1
cast receipt --rpc-url "$RPC_URL" "$TX_HASH"
unset TEST_KEY RAW_TX TX_HASH
```

This works because every new address receives the test-only initial balance
and has nonce zero. Use a new key each time so the explicit nonce remains
correct.

### Fetch a transaction with `cast tx`

`cast tx` works for a known finalized transaction hash, decoding it the same
way `eth_sendRawTransaction` decoded it on the way in:

```sh
cast tx \
  --rpc-url http://127.0.0.1:8545 \
  0xYOUR_TRANSACTION_HASH
```

Like `eth_getTransactionReceipt`, a lookup for a pending or unknown hash
returns `null` rather than a pending-shaped result: this RPC tracks no local
mempool to resolve a pending transaction from.

### Fetch the nonce, block height, and chain ID with `cast`

`cast nonce`, `cast block-number`, and `cast chain-id` all work against the
EVM-only RPC:

```sh
cast nonce --rpc-url http://127.0.0.1:8545 0xYOUR_ADDRESS
cast block-number --rpc-url http://127.0.0.1:8545
cast chain-id --rpc-url http://127.0.0.1:8545
```

`eth_getTransactionCount` accepts the `latest`, `safe`, `finalized`, and
`pending` block tags, but all four resolve to the current committed nonce.
`pending` is accepted so standard tooling that requests it (`cast send`,
ethers, viem) keeps working, not because instant finality makes committed and
pending equivalent: instant finality removes reorg risk, not the
broadcast-to-commit window `pending` exists to cover. Two transactions sent
back-to-back from the same key before the first commits are therefore
assigned the same nonce, and the second is rejected; callers issuing rapid
sequential sends must track the next nonce themselves rather than relying on
`pending`. An explicit height, an explicit hash, or `earliest` returns an
error: historical state is not available from this RPC. `eth_blockNumber` and
`eth_chainId` take no block selector and always return the current height and
the network's configured EVM chain ID.

### Make a read-only call with `cast call`

`cast call` executes a message against current committed state without
sending a transaction, so it works for any `view`/`pure` contract function,
such as an ERC20 `balanceOf`:

```sh
cast call \
  --rpc-url http://127.0.0.1:8545 \
  0xYOUR_CONTRACT_ADDRESS \
  "balanceOf(address)(uint256)" \
  0xYOUR_ADDRESS
```

`eth_call` accepts the same `latest`/`safe`/`finalized`/`pending` block tags as
`eth_getBalance` and `eth_getTransactionCount`; an explicit height, an explicit
hash, or `earliest` returns the same historical-state error. A caller-omitted
gas limit defaults to a fixed cap rather than the block gas limit, and an
explicit limit above that cap is silently lowered to it. A reverted call
returns a JSON-RPC error carrying the ABI-decoded revert reason, matching
go-ethereum's own `eth_call` behavior.

### Estimate gas with `cast estimate`

```sh
cast estimate \
  --rpc-url http://127.0.0.1:8545 \
  0xYOUR_CONTRACT_ADDRESS \
  "transfer(address,uint256)" 0xYOUR_ADDRESS 1
```

`eth_estimateGas` runs the same read-only execution as `eth_call` and searches
for the lowest gas limit at which it succeeds, to within 1.5% as go-ethereum
does. A plain value transfer to an account without code returns exactly 21000
after a single execution. The upper bound is the caller's `gas` if given,
otherwise the same fixed cap `eth_call` applies, lowered further to what the
sender's balance can pay for when a non-zero `gasPrice`/`maxFeePerGas` is
supplied. A message that reverts even at that bound returns the revert error
(code 3, ABI-decoded reason); one that only runs out of gas returns "gas
required exceeds allowance". The block selector is optional and accepts the
same tags as `eth_call`, with the same historical-state error otherwise.

### Fetch contract code with `cast code`

```sh
cast code --rpc-url http://127.0.0.1:8545 0xYOUR_CONTRACT_ADDRESS
```

`eth_getCode` accepts the same `latest`/`safe`/`finalized`/`pending` block tags
as `eth_getBalance`; an explicit height, an explicit hash, or `earliest`
returns the same historical-state error. An address with no code, including an
EOA or an address that was never touched, returns `0x`, as go-ethereum does.

Block context for a call is a mix of real and best-effort values: `Number` and
`GasLimit` are the actual current committed values, but `Coinbase` is always
the zero address (this application never sets one, even for committed
blocks) and `blockhash(current-1)` and further back are unavailable (only the
current block's own hash is tracked outside of block execution). A view
function that depends on either reads a placeholder rather than a real value.

### Fetch a block with `cast block`

`cast block` works by height or by hash. Unlike `eth_getBalance`,
`eth_getTransactionCount`, and `eth_call`, an explicit height is not
historical-state-restricted: any past height still within the node's
retention window works the same as `latest`:

```sh
cast block --rpc-url http://127.0.0.1:8545 latest
cast block --rpc-url http://127.0.0.1:8545 1
cast block --rpc-url http://127.0.0.1:8545 0xYOUR_BLOCK_HASH
```

A height above the current chain head, `earliest` (this executor's first
committed height is 1, not 0), or a height the node has since pruned all
return `null` rather than an error, matching `eth_getTransactionByHash`'s
treatment of an unknown hash. `nonce`, `mixHash`, `sha3Uncles`, `difficulty`,
`extraData`, `uncles`, and `totalDifficulty` are always their
Ethereum-inapplicable zero value, matching `eth_getBlockByNumber` on the
regular (non-EVM-only) RPC. `logsBloom` is always empty, unlike the regular
RPC, which aggregates it from a receipt per transaction. `gasUsed` and the
transaction list are real, decoded the same way `eth_getTransactionByHash`
decodes a transaction, but `gasUsed` is scoped to the one Autobahn lane this
block belongs to: four lanes execute concurrently, each advancing its own
block sequence, so this total does not cover every lane's activity at this
point in the chain. Revisit once superblocks merge lanes into a single
block; punted for now since a block today is exactly one lane's
transactions.

The remaining `cast` gaps are RPC gaps, not receipt-decoding gaps. `sei-load`
does not currently print every submitted hash, and there are still no
by-block-and-index transaction lookups or block-transaction-count methods.
There are also no fee-estimation, gas-estimation, log, or WebSocket
subscription methods. Commands that depend on those queries cannot operate
normally; raw transactions must provide gas limit and gas price offline as in
the example above.

## Tear down

Stop the local containers and remove their manager metadata:

```sh
./autobahn-e2e teardown --name autobahn-evmonly
```

Stop an AWS cluster and remove the EC2 instance, security group, managed key
pair, local managed private key, and manager metadata:

```sh
./autobahn-e2e teardown --name my-autobahn
```

If provisioning fails after resources are created, the cluster is retained in
`failed` state so it can still be inspected with `list` and removed with
`teardown`.
