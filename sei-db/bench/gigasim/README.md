The `gigasim` benchmark drives a Giga node's whole storage stack end to end. Where
[`blocksim`](../blocksim) exercises the block ledger alone and [`cryptosim`](../cryptosim) exercises the
state DB alone, gigasim runs both together with the receipt store, through the same
`GigaStorageManager` a node opens. It is the benchmark to reach for when the question is how the
engines behave *together* — whether pruning, checkpointing and hashing on one store show up as latency
on another.

# Running Gigasim

Run from anywhere in the repository; the script builds what it needs first:

```
./sei-db/bench/gigasim/gigasim.sh ./sei-db/bench/gigasim/config/standard.json
```

Three configurations ship with the benchmark:

| Config | Shape it simulates |
| --- | --- |
| `config/standard.json` | A full node: block store, live state DB, historical state store, receipts |
| `config/validator.json` | A validator: block store and live state DB only |
| `config/debug.json` | A small, short local run for smoke testing |

A run continues until interrupted. Stop it with Ctrl-C, which shuts down gracefully and leaves the data
directory resumable; set `MaxRuntimeSeconds` to have it stop on its own instead. Press Enter while a
run is in progress to suspend it, and again to resume — set `EnableSuspension` to false when running
somewhere without a terminal attached.

# How It Works

Two goroutines split the work the way a node splits consensus from execution.

**The generator** builds a block's transactions, writes the block to the block ledger, and hands it to
execution over a channel. **The consumer** takes each block off that channel, runs its transactions
across the executor pool, and writes what they produced to the receipt store and the state DB. Storing
a block and executing one therefore overlap, and `StagedBlockQueueSize` bounds how far generation may
run ahead.

```
generator ──build txs──> write to BlockDB ──> [channel] ──> executor pool ──> receipts + state DB
```

Every block commits at the same height in every store. A block's transactions are executed in
parallel across the pool, so a block's transaction count is also its degree of parallelism, and the
pool is drained before the block is committed — a block's writes reach state as one version.

## Transaction Model

Execution is simulated, not real: the benchmark replays the reads and writes an ERC20 transfer makes
without doing the arithmetic, because what is under measurement is storage traffic rather than the EVM.
Each transaction reads the contract code, both accounts, both storage slots and the fee account, then
writes both accounts, both slots and the fee account. This is the same model `cryptosim` uses, so the
two are comparable on the state DB.

Accounts are drawn from a hot set chosen most of the time, a cold set chosen occasionally and growing
as the run mints accounts, and a dormant set that is never chosen and exists only to give the state DB
a realistic resident size.

## Setup

Before measurement starts, the benchmark creates the configured ERC20 contracts and account population.
Those setup blocks go through the same stores at the same heights as measured blocks, and they are
excluded from the reported rates. Setup is skipped when the data directory already holds the
population, which is what makes a large data set worth keeping between runs.

# Configuring Gigasim

Every option and its default live in the [gigasim config struct](./gigasim_config.go). Fields in the
JSON file mirror the struct field names exactly, and an unrecognised field is an error rather than a
silent no-op. Anything left out takes its default.

The knobs that shape a run:

| Field | Default | Meaning |
| --- | --- | --- |
| `TransactionsPerBlock` | 2000 | Transactions per block, and the parallel batch size |
| `BytesPerTransaction` | 1024 | Payload bytes per transaction on the ledger's write path |
| `BlocksPerSecond` | 100 | Generation ceiling; 0 runs as fast as the stack allows |
| `StagedBlockQueueSize` | 8 | How many blocks generation may run ahead of execution |
| `NumberOfHotAccounts` | 100 | Accounts chosen most often |
| `MinimumNumberOfColdAccounts` | 100,000 | Accounts chosen occasionally |
| `MinimumNumberOfDormantAccounts` | 100,000 | Accounts never chosen, resident only |
| `HotAccountProbability` | 0.5 | Chance a selection comes from the hot set |
| `NewAccountProbability` | 0.01 | Chance a non-hot selection mints an account |
| `RollbackWindow` | 1,000 | Blocks of history every store keeps for rollback |
| `LookbackWindow` | 0 | Queryable history below the rollback window; -1 keeps everything |
| `PruneIntervalSeconds` | 300 | How often the garbage collector runs |
| `CheckpointIntervalSeconds` | 600 | Wall-clock gap between checkpoints; 0 disables |
| `CheckpointBlockInterval` | 0 | Checkpoint only at multiples of this height; 0 accepts any |
| `EnableStateStore` | true | Whether the historical EVM state store is opened at all |
| `EnableReceiptStore` | true | Whether the receipt store is opened at all |
| `ThreadsPerCore` | 2 | Executor threads per core, plus `ConstantThreadCount` |

The default block is the largest consensus accepts: `TransactionsPerBlock` sits at autobahn's
`MaxTxsPerBlock`, and `TransactionsPerBlock * BytesPerTransaction` is exactly `MaxTxsBytesPerBlock`.
Raising either one therefore means lowering the other, and configuration validation rejects the
combination rather than generating a block the ledger would refuse.

The default `BlocksPerSecond` is a ceiling rather than a target. At the default block size it stands
for 200,000 transactions a second, which is well above what the stack sustains today, so a run against
the default configuration is not in practice throttled.

## Optional Stores

`EnableStateStore` and `EnableReceiptStore` control whether those stores exist, not merely whether they
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

Metrics are served for Prometheus at `MetricsAddr` (`:9090` by default; empty disables the server). All
instruments are prefixed `gigasim_`, and cover per-store throughput, the staged block queue depth, the
account population, block hash wait time, and a phase breakdown for the generator thread, the consumer
thread and the executors.

The queue depth is the first number to read when interpreting a run: a queue that stays full means
execution and the state DB are the limit, and one that stays empty means generation or the block ledger
is.

For local Prometheus and Grafana containers, see the corresponding section of the
[cryptosim README](../cryptosim/README.md#setting-up-prometheus--grafana); the setup is the same.
Grafana provisions every dashboard in `docker/monitornode/dashboards`, so the run appears under
**GigaSim** without any import step.

# Tests

The package's tests open real stores, so run them on a RAM disk:

```
scripts/ramtest.sh ./sei-db/bench/gigasim/... -race
```
