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
- `eth_getBalance`, for the current committed EVM balance.

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

Without `--async`, `cast receipt` polls until the receipt exists. While it is
waiting, current Foundry versions may also poll `eth_blockNumber` and print a
method-not-found error, although the command still returns the receipt after
finalization. Add `--async` for a one-shot lookup that fails immediately when
the hash is not found. Confirmation counting is not available without
`eth_blockNumber`; the endpoint itself only returns finalized receipts.

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

The remaining `cast` gaps are RPC gaps, not receipt-decoding gaps. There is no
`eth_getTransactionByHash` or block API to discover a `sei-load` transfer hash,
and `sei-load` does not currently print every submitted hash. There are also no
chain ID, nonce, fee-estimation, gas-estimation, call, log, or WebSocket
subscription methods. Commands that depend on those queries cannot operate
normally; raw transactions must provide chain ID, nonce, gas limit, and gas
price offline as in the example above.

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
