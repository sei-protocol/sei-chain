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
a block and executing one therefore overlap, and `MaxPendingExecutionQueueSize` bounds how far
generation may run ahead.

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

Metrics are served for Prometheus at `MetricsAddr` (`:9090` by default; empty disables the server). The
benchmark's own instruments are prefixed `gigasim_` and cover per-store write volume, the staged block
queue depth, the account population, on-disk size per store, block hash wait time, and a phase
breakdown for the generator thread, the consumer thread and the executors. The stores served on the
same endpoint publish their own: `flatkv_`, `seiwal_`, `litt_`, `pebble_` and `giga_state_commit_`.

`LittMetricsEnabled` controls the last of those for the two LittDB-backed stores, the block ledger and
the receipt store. It is on by default and is the only source of their size and queue depth.

## Reading a run

`gigasim_lifecycle_phase_duration_seconds_total` and `giga_state_commit_phase_duration_seconds_total`
together break one block's latency into the stages it blocks in — generating it, writing it to the
ledger, executing it, writing its receipts, and the three stores the commit fans out to. Only blocking
work is counted, so waiting for another goroutine is excluded and an asynchronous store contributes the
wait to hand the block over rather than the write itself. Stacked, they sum to the critical path of one
block, which is what makes the tallest band the thing to fix.

The queue depths say which stage is applying the backpressure: one that stays full is the limit, and
one that stays empty means the stage feeding it is.

For local Prometheus and Grafana containers, see the corresponding section of the
[cryptosim README](../cryptosim/README.md#setting-up-prometheus--grafana); the setup is the same.
Grafana provisions every dashboard in `docker/monitornode/dashboards`, so the run appears under
**GigaSim** without any import step.

# Tests

The package's tests open real stores, so run them on a RAM disk:

```
scripts/ramtest.sh ./sei-db/bench/gigasim/... -race
```
