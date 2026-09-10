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

Setup creates exactly the cold and dormant counts the config asks for, assigning each account to a set
by its identifier: the hot accounts take the lowest identifiers, the dormant accounts follow, and the
cold accounts take the highest, which is the range cold selection draws from.
`NewAccountDormancyProbability` governs only the accounts minted during the run itself.

# Configuring Gigasim

Every option, its default and what it means live in the [gigasim config struct](./gigasim_config.go),
which is the reference for them rather than this document. Fields in the JSON file mirror the struct
field names exactly, and an unrecognised field is an error rather than a silent no-op. Anything left
out takes its default, so the shipped configs in [`config/`](./config) set only what they change and
read as worked examples.

Two relationships between the options are worth knowing before changing any of them, because neither is
visible from a single field.

The default block is the largest consensus accepts: `TransactionsPerBlock` sits at autobahn's
`MaxTxsPerBlock`, and `TransactionsPerBlock * BytesPerTransaction` is exactly `MaxTxsBytesPerBlock`.
Raising either one therefore means lowering the other, and configuration validation rejects the
combination rather than generating a block the ledger would refuse.

Generation is unthrottled by default, so a measured run reports what the stack sustains rather than a
rate chosen in advance. `MaxBlocksPerSecond` exists for the runs that are not measurements — the debug
config throttles itself well below what a machine can do, because a smoke test should confirm the
pipeline works rather than saturate the laptop it runs on — and for holding two builds at the same
offered load, which is what makes their latencies comparable.

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
