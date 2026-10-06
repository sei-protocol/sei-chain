The `gigasim` benchmark drives a Giga node's whole storage stack end to end: the block ledger, the state
DB and the receipt store, through the same `GigaStorageManager` a node opens. It measures each engine
under the traffic a node gives it, and how the engines behave *together* — whether pruning,
checkpointing and hashing on one store show up as latency on another.

Gigasim does not start validators or accept RPC traffic. For a four-validator Autobahn EVM-only
cluster, local or on AWS, use [`autobahn-e2e`](../../../integration_test/autobahn/README.md). AWS
deploy takes `--topology distributed` (default: one validator per EC2, plus a load/monitoring host)
or `--topology colocated` (all four Docker validators on a single EC2).

# Running Gigasim

Run from anywhere in the repository; the script builds what it needs first:

```
./sei-db/bench/gigasim/gigasim.sh ./sei-db/bench/gigasim/config/full-node.json
```

Four configurations ship with the benchmark:

| Config | Shape it simulates |
| --- | --- |
| `config/full-node.json` | A full node: block store, live state DB, historical state store, receipts |
| `config/archive-node.json` | An archive node: a full node that keeps all history, never pruning it |
| `config/validator.json` | A validator: block store and live state DB only |
| `config/debug.json` | A small, short local run for smoke testing |

A run continues until interrupted. Stop it with Ctrl-C, which shuts down gracefully and leaves the data
directory resumable; set `MaxRuntimeSeconds` to have it stop on its own instead. Press Enter while a
run is in progress to suspend it, and again to resume — set `EnableSuspension` to false when running
somewhere without a terminal attached.

Everything a run writes lives under `DataDir`, including its log: `logs/gigasim.log` beneath the data
directory. `CleanDataOnStart` and `CleanDataOnExit` clean the logs along with the data.

# Hashing Kernel

`gigasim.sh` builds with `GOEXPERIMENT=simd`, which compiles in the AVX-512 LtHash kernel. The
kernel is selected automatically on a host with AVX-512F and VBMI2, and the portable Go kernel is used
everywhere else. The run prints which one it got:

```
lthash backend: simd
```

To measure against the portable kernel, pin it at run time — both are in the binary, so no rebuild is
needed:

```
SEI_LTHASH_BACKEND=default ./sei-db/bench/gigasim/gigasim.sh ./sei-db/bench/gigasim/config/full-node.json
```

Building through the Makefile directly rather than through `gigasim.sh` sets no experiment, and
produces a binary that only has the portable kernel. `seid` is built that way too: production does not
run the AVX-512 kernel.

# How It Works

Two goroutines split the work the way a node splits consensus from execution.

**The generator** builds a block's transactions, writes the block to the block ledger, and hands it to
execution over a channel. **The consumer** takes each block off that channel, runs its transactions
across the executor pool, and writes what they produced to the receipt store and the state DB. Storing
a block and executing one therefore overlap, and `MaxPendingExecutionQueueSize` bounds how far
generation may run ahead.

```
generator ──build txs──> write to BlockDB ──> [channel] ──> executor pool ──> receipts + state DB
```

A superblock's transactions are executed in parallel across the pool, so its transaction count is
also its degree of parallelism, and the pool is drained before the superblock is committed — its
writes reach state as one version. The default superblock is 50 lane blocks of 2,000 transactions.
The block store's height counts lane blocks, and the state DB and receipt store count superblocks.

## Transaction Model

Execution is simulated, not real: the benchmark replays the reads and writes a transfer makes without
doing the arithmetic, because what is under measurement is storage traffic rather than the EVM.
`TransactionType` picks the kind of transfer every block carries:

| `TransactionType` | Reads | Writes | Gas |
| --- | --- | --- | --- |
| `erc20` (default) | token code, sender's account, sender's and recipient's balance slots, fee account | sender's account, both balance slots | `Erc20GasPerTransaction` (50,000) |
| `transfer` | sender's and recipient's accounts, fee account | both accounts | 21,000 |

The fee account is written once per superblock, not once per transaction. Accounts are read and written
through their native balance, which every transaction changes for the sender paying gas. An ERC20
transfer never touches the recipient's account, as on chain: it names the recipient only as an argument
to the token contract. A native transfer touches no contract code or storage.

Balance slots live in the token's storage, keyed by the token contract's address and then a slot that
follows the holder, so a token's balances share its address as a key prefix and a hot token is a hot
key range. Each account holds `Erc20InteractionsPerAccount` tokens, drawn from the hot set on
`HotErc20ContractProbability` of holdings, and a transfer moves one of the sender's holdings. The
recipient may not hold that token yet, in which case the transfer creates its balance slot, as a
payment to a first-time holder does.

A native transfer's 21,000 gas is the EVM's fixed transaction cost. An ERC20 transfer between existing
holders uses roughly 35,000 to 50,000 gas and one to a first-time holder around 55,000 to 65,000, which
is what `Erc20GasPerTransaction` averages over. Every transaction's receipt records the same gas, so the
receipts, each block's gas totals and `gigasim_gas_used_total` all agree.

Accounts are drawn from a hot set chosen most of the time, a cold set chosen occasionally, and a
dormant set that is never chosen and exists only to give the state DB a realistic resident size. All
three grow as the run mints accounts, in the proportions `NewAccountHotProbability` and
`NewAccountDormantProbability` describe, with the remainder becoming cold.

## Setup

Before measurement starts, the benchmark creates the configured ERC20 contracts and account population.
Those setup blocks go through the same stores at the same heights as measured blocks, and they are
excluded from the reported rates. Setup is skipped when the data directory already holds the
population, which is what makes a large data set worth keeping between runs.

Setup creates exactly the counts the config asks for, and an account's set follows from the identifier
it takes: the hot accounts take the lowest identifiers, the dormant accounts follow, and the cold
accounts take the highest. Accounts minted during the run take the identifiers above those, where the
same rule continues — each block of a thousand identifiers is split between the three sets in the
configured proportions.

Deriving the set from the identifier is what keeps selection honest. Nothing about the population is
counted as the run goes, so there is no tally to drift from what the identifiers say, and a resumed
run reconstructs the same population from its config and the persisted identifier counter alone.

# Configuring Gigasim

Every option, its default and what it means live in the [gigasim config struct](./gigasim_config.go),
which is the reference for them rather than this document. Fields in the JSON file mirror the struct
field names exactly, and an unrecognised field is an error rather than a silent no-op. Anything left
out takes its default. The shipped configs in [`config/`](./config) set `TransactionsPerBlock` and
`LaneBlocksPerSuperblock` explicitly, and otherwise only what they change.

A few relationships between the options are worth knowing before changing any of them, because none is
visible from a single field.

The default lane block carries 2,000 transactions of 200 bytes, one per ledger entry, which is
autobahn's `MaxTxsPerBlock`. A transaction of 200 bytes is about the size of an ERC20 transfer: some
180 bytes of RLP before Sei's envelope. A lane block may carry more transactions than it has entries
by packing several into each entry. Packing leaves the bytes stored unchanged, and those are held to
the ledger's byte budget, `MaxTxsBytesPerBlock` (2 MiB). `TransactionsPerBlock` and
`BytesPerTransaction` trade off against each other: configuration validation rejects a product over
the budget rather than generating a block the ledger would refuse.

`LaneBlocksPerSuperblock` bundles that many lane blocks into one superblock, which is what gets
executed and committed. The default is 50, so a superblock is 100,000 transactions. The block store
advances once per lane block; the state DB and the receipt store advance once per superblock.
`RollbackWindow` and `LookbackWindow` count each store's own versions, so the block store's window is
lane blocks and the state DB's is superblocks. The debug config sets `LaneBlocksPerSuperblock` to 1,
which commits each of its lane blocks on its own.

Generation is unthrottled by default, so a measured run reports what the stack sustains rather than a
rate chosen in advance. `MaxTps` exists for the runs that are not measurements — the debug config
throttles itself well below what a machine can do, because a smoke test should confirm the pipeline
works rather than saturate the laptop it runs on — and for holding two builds at the same offered load,
which is what makes their latencies comparable. Blocks are released a whole superblock at a time, so the
block rate it produces is `MaxTps / (TransactionsPerBlock * LaneBlocksPerSuperblock)`.

## Optional Stores

`EnableSS` and `EnableReceiptStore` control whether those stores exist, not merely whether they
are written: a disabled store is never opened, no receipts are built for it, and it leaves no directory
behind. Turning both off is what `config/validator.json` does, and it is how to measure a validator's
stack rather than a full node's.

# Data Persistence

The benchmark writes real database files and reuses them: pointed at a populated data directory it
resumes where the previous run stopped, skipping setup. That matters for large populations, where setup
dominates a short run.

A run must end cleanly to leave a resumable directory. Both Ctrl-C and `MaxRuntimeSeconds` drain the
blocks still in flight, so the ledger and the state DB come to rest on the same height. A run that dies
without draining — a `kill -9`, a crash, a power loss — leaves the ledger ahead instead: recovery cuts
state and receipts back to a common height but never rolls the ledger back, and this benchmark
generates blocks rather than replaying them, so it cannot close that gap. Startup reports it, and the
directory has to be cleaned; `CleanDataOnStart` does that for you.

Do not change `Seed` or `CannedRandomSize` when continuing from an earlier run. Keys are derived
deterministically from them, so a changed value makes the benchmark address a population that is not
the one on disk.

# Metrics

Metrics are served for Prometheus at `MetricsAddr` (`:9090` by default; empty disables the server). The
benchmark's own instruments are prefixed `gigasim_` and cover per-store write volume, the pending
execution queue, the account population, on-disk size per store, block hash wait time, and a phase
breakdown for the generator thread, the consumer thread and the executors. The stores served on the
same endpoint publish their own: `flatkv_`, `seiwal_`, `litt_`, `pebble_` and `giga_state_commit_`, and
the garbage collector pruning them publishes `storage_gc_`: its cut lines, each store's rollback floor,
and how long each store took to prune.

`LittMetricsEnabled` controls the last of those for the two LittDB-backed stores, the block ledger and
the receipt store. It is on by default and is the only source of their size and queue depth.

## Reading a run

Two goroutines each account for all of their own time.
`gigasim_execution_loop_phase_duration_seconds_total`, together with
`giga_state_commit_phase_duration_seconds_total` for the commit window it hands to the state DB,
covers the goroutine that executes and commits blocks;
`gigasim_block_producing_loop_phase_duration_seconds_total` covers the one that builds blocks and
writes them to the block store. Each set sums to 100% of its own goroutine, so the two are shares of
different denominators and do not add up to anything between them.

Four more break a single phase down further, each summing to the phase it subdivides:
`gigasim_transaction_` for execution, `gigasim_receipt_write_` for the receipt write,
`gigasim_blockstore_write_` for the ledger write, and `ss_evm_commit_` for the EVM state store's share
of the commit.

Read the waiting phase first. `wait_for_execution` on the producing loop and `wait_for_block` on the
execution loop are each goroutine's idle time, so the one with almost none is the bottleneck and the
one with plenty has headroom. The per-block totals are not a second opinion on this: both equal
`1 / throughput` by construction, whatever the split between work and waiting.

`{prefix}_queue_blocked_seconds_total` says which queue is applying the backpressure — it accumulates
the time producers spent waiting for room, so a queue nobody waits on reports nothing. The depth
gauges beside it are sampled on a timer and show how full a queue sits in the ordinary case, which a
queue that fills only in bursts will understate.

## Prometheus and Grafana

To run local Prometheus and Grafana containers, run the following from the repository root, with Docker
installed:

```
docker/monitornode/scripts/start-prometheus.sh
docker/monitornode/scripts/start-grafana.sh
docker/monitornode/scripts/start-node-exporter.sh
```

Grafana is at http://localhost:3000/, with username and password `admin`. It provisions every dashboard
in `docker/monitornode/dashboards`, so the run appears under **GigaSim** without any import step.
Prometheus scrapes gigasim at the default `MetricsAddr`. Stop the containers with the matching
`stop-*.sh` scripts.

# Running on AWS

1. Clone the repository and install the dependencies (Go, build tools, tmux and Docker) on an Ubuntu
   host:

   ```
   git clone https://github.com/sei-protocol/sei-chain.git
   sudo ./sei-chain/sei-db/bench/gigasim/tools/setup-ubuntu.sh
   ```

2. Optionally, start Prometheus on the host: `./sei-chain/docker/monitornode/scripts/start-prometheus.sh`.

3. Start the benchmark, inside tmux so that it survives a dropped connection:

   ```
   ./sei-chain/sei-db/bench/gigasim/gigasim.sh ./sei-chain/sei-db/bench/gigasim/config/full-node.json
   ```

4. Optionally, view the remote run in a local Grafana. With Prometheus running on the remote host and
   Grafana (but not Prometheus) running locally, open an SSH tunnel to the remote Prometheus and keep it
   open:

   ```
   ssh -L 9091:localhost:9091 user@remote-host
   ```

# Profiling

Profiling is off by default. Set `PprofAddr` (for example `":6060"`) to serve the pprof endpoints. They
come up before the storage opens, so opening storage and setup can be profiled as well as the run:

```
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

The mutex and block profiles are off unless `MutexProfileFraction` or `BlockProfileRate` turns them on.
Both slow what they sample, so a run with either on is for diagnosis rather than measurement.

# Tests

The package's tests open real stores, so run them on a RAM disk:

```
scripts/ramtest.sh ./sei-db/bench/gigasim/... -race
```
