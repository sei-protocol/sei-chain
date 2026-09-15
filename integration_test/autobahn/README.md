# Autobahn EVM-only E2E clusters

`autobahn-e2e` manages the four-validator, disk-backed EVM-only Autobahn
topology used for integration and load testing. Locally it runs all four
validators in Docker on one host. On AWS it places each validator on its
own EC2 instance and adds a fifth instance that scrapes metrics. Start
`sei-load` on that host when you want traffic.

Run every command in this document from the root of a `sei-chain` checkout.
The examples use the default cluster name, `autobahn-evmonly`. If `--name` is
passed to `deploy`, pass the same name to `list`, `forward`, and `teardown`.

## Prerequisites

Both targets require Go 1.25.6 and `make`. Local deployment also requires a
running Docker engine with Docker Compose v2. AWS deployment requires the AWS
CLI, `git`, and `ssh`, plus credentials allowed to manage EC2 instances,
security groups, and key pairs. The inspection and receipt examples also use
`jq` and Foundry's `cast`.

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

AWS deploy has two topologies, selected with `--topology`:

- `distributed` (default): five Ubuntu EC2 hosts — four validators and one
  load/monitoring instance. Each validator runs a single `seid` container
  that advertises the instance's private IP. The four validators clone,
  compile, and initialize in parallel. The load instance is brought up
  afterward with Prometheus and Grafana; `sei-load` is left for you to
  start. The security group admits SSH from the caller, Grafana (`:3000`)
  from the internet, and all TCP between the five instances.
- `colocated`: one Ubuntu EC2 host running the same four-container Docker
  topology used locally, plus Prometheus and Grafana. Use this when you
  want the cheaper single-instance setup.

EVM JSON-RPC stays off the public internet and is accessed through
`forward`.

```sh
./autobahn-e2e deploy --target aws \
  --name my-autobahn \
  --region us-west-2 \
  --topology distributed

./autobahn-e2e deploy --target aws \
  --name my-autobahn-colo \
  --region us-west-2 \
  --topology colocated

./autobahn-e2e list --name my-autobahn
```

Deploy prints the public Grafana URL (`http://<public-ip>:3000`, admin /
admin). `list` repeats it under `DASHBOARD`. Open **Autobahn E2E**. The
login is the default Grafana pair on a temporary test host; tear the
cluster down when finished.

Deploy writes `integration_test/autobahn/sei-load.aws.json` on the load
instance with `http://<validator-private-ip>:8545` for every validator.
It does not start `sei-load`. When you want traffic, SSH in and run it:

```sh
ssh -i ~/.sei/autobahn-e2e/my-autobahn.pem ubuntu@<load-public-ip>
cd ~/sei-chain-my-autobahn
GOBIN="$PWD/build/tools" go install github.com/sei-protocol/sei-load@v0.0.1
./build/tools/sei-load \
  --config integration_test/autobahn/sei-load.aws.json \
  --metricsListenAddr 0.0.0.0:19698
```

In another terminal, forward one validator to the laptop:

```sh
./autobahn-e2e forward \
  --name my-autobahn \
  --node node-0 \
  --local-port 18545
```

One forwarded endpoint is sufficient for `cast` and similar tools:
validator EVM proxying is enabled by default, so transactions submitted
to node 0 are forwarded to the Autobahn validator that owns the sender's
shard.

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

The default security-group rule admits SSH from the public IP detected at
deployment time, and Grafana from `0.0.0.0/0`. Use `--ssh-cidr` when a VPN,
NAT, or IPv6 setup makes the SSH source incorrect. Use `--subnet-id` if the
region has no default VPC or the instance needs a specific public subnet.

The default validator instance is `r7i.12xlarge` with 1024 GiB of gp3 storage
(10000 IOPS, 1000 MB/s) and the
current Ubuntu 24.04 AMD64 AMI from AWS Systems Manager. The load instance
uses the same AMI and instance type with a 100 GiB gp3 root volume. Override
the validator disk with `--volume-size`, `--volume-iops`, and
`--volume-throughput`. When changing architecture, override `--instance-type`
and `--ami-id` together. `--timeout` defaults to 40 minutes to cover the
image build, `seid` compile, and five-instance bootstrap.
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
four-endpoint configuration. An AWS deploy writes
`integration_test/autobahn/sei-load.aws.json` on the load instance with the
four private EVM URLs and leaves `sei-load` stopped. To drive load from
the laptop instead, copy the local file and point `endpoints` at one or
more `forward` tunnels.

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

## Watch the dashboard

An AWS deploy starts Prometheus and Grafana on the load instance and prints
a public URL. Open that address (admin / admin) and select **Autobahn E2E**.
Prometheus scrapes each validator at `<private-ip>:26660`.

For a local cluster, start the monitornode containers after the nodes are
up. Prometheus scrapes each validator at `:26660` and Grafana provisions
**Autobahn E2E** from `docker/monitornode/dashboards`.

```sh
docker/monitornode/scripts/start-prometheus.sh
docker/monitornode/scripts/start-grafana.sh
```

Open http://localhost:3000 (admin / admin) and select **Autobahn E2E**.
The overview line is executed TPS, blocks/sec, and produce-to-execute
finalize time. The pie and stacked line are the execute goroutine split
across consensus wait, EVM execution, and storage.

If Prometheus was already running from a gigasim or cryptosim session,
run `start-prometheus.sh` again after the cluster is up so it joins the
node network and reloads scrape targets.

`make docker-cluster-start-monitoring` provisions the same dashboard
(Grafana at http://localhost:3000, Prometheus UI at http://localhost:9099).

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
- `eth_getTransactionReceipt`, for finalized receipts.

All other `eth_*` methods currently return JSON-RPC method-not-found. A lookup
for a pending or unknown hash returns `null`.

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
chain ID, balance, nonce, fee-estimation, gas-estimation, call, log, or
WebSocket subscription methods. Commands that depend on those queries cannot
operate normally; raw transactions must provide chain ID, nonce, gas limit,
and gas price offline as in the example above.

## Tear down

Stop the local containers and remove their manager metadata:

```sh
./autobahn-e2e teardown --name autobahn-evmonly
```

Stop an AWS cluster and remove the five EC2 instances, security group, managed key
pair, local managed private key, and manager metadata:

```sh
./autobahn-e2e teardown --name my-autobahn
```

If provisioning fails after resources are created, the cluster is retained in
`failed` state so it can still be inspected with `list` and removed with
`teardown`.
