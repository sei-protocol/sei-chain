# evmonly-loadtest

`evmonly-loadtest` is a standalone executable for feeding synthetic blocks to
the EVM-only executor through an in-memory `giga.Store`, without Cosmos SDK
state, mempool, RPC, or production SC/SS persistence.

The synthetic workload defaults to local EVM chain ID `1337`; override it with
`--chain-id` when testing another signing domain.

It currently generates pure EVM legacy transfer transactions, ERC20 transfer
transactions using `sei-load`'s compiled contract runtime, and a contract-call
workload that exercises nested StateDB
snapshot/revert behavior. By default, each generated sender account has one
nonce-0 transaction and is funded in the command's in-memory genesis state
before its block is queued. Recipients are unique by default so the transfer
workloads exercise the optimistic no-overlap case. Pass
`--recipient-conflict-rate=<0..1>` to pair that fraction of each block's
transactions onto shared recipients, or pass `--recipient=0x...` to force all
transactions to a single recipient. Pass `--same-sender` to use one sender per
block with sequential transaction nonces.

Run a bounded prebuilt test:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest --blocks=1000 --txs-per-block=1000
```

Example local saturation run:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --metrics-addr= \
  --report-interval=5s \
  --blocks=2000 \
  --txs-per-block=1000 \
  --builders=16 \
  --workers=1 \
  --executor-workers=12 \
  --gas-price-wei=0 \
  --min-gas-price-wei=0 \
  --queue-size=512
```

The three executor OCC scenarios have direct command-line forms:

```bash
# Conflict-free: unique senders and recipients.
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --blocks=400 \
  --gas-price-wei=0 --min-gas-price-wei=0

# Hot recipient: unique senders all credit one account.
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --blocks=400 \
  --gas-price-wei=0 --min-gas-price-wei=0 \
  --recipient=0x00000000000000000000000000000000000000f1

# Same-sender nonce chain: one sender and sequential nonces within each block.
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --blocks=400 \
  --gas-price-wei=0 --min-gas-price-wei=0 \
  --same-sender
```

Example conflict run:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --metrics-addr= \
  --report-interval=5s \
  --blocks=400 \
  --txs-per-block=5000 \
  --builders=16 \
  --workers=1 \
  --executor-workers=12 \
  --gas-price-wei=0 \
  --min-gas-price-wei=0 \
  --recipient-conflict-rate=0.10
```

Example snapshot/revert contract-call run:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --metrics-addr= \
  --report-interval=5s \
  --workload=snapshot-revert \
  --blocks=400 \
  --txs-per-block=5000 \
  --builders=16 \
  --workers=1 \
  --executor-workers=12 \
  --gas-price-wei=0 \
  --min-gas-price-wei=0
```

The command prebuilds every bounded run before starting the prepare/recover and
executor workers, which isolates executor throughput from block generation:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --metrics-addr= \
  --report-interval=5s \
  --blocks=400 \
  --txs-per-block=5000 \
  --builders=48 \
  --workers=1 \
  --executor-workers=24 \
  --gas-price-wei=0 \
  --min-gas-price-wei=0 \
  --queue-size=512
```

Prebuilding requires `--blocks > 0` and stores every raw block in memory. Sender
recovery still runs in the measured phase, but it is pipelined ahead of
execution through `--prepare-workers`.

The zero gas price/min-gas settings keep the conflict-free transfer workload
focused on the optimistic no-overlap case. Non-zero fees make every transaction
update the same coinbase balance, which is a real intra-block conflict.

Useful knobs:

- `--blocks`: number of blocks to prebuild and execute. This is required and
  must be greater than `0`.
- `--workers`: ordered block executor workers. This must be `1` because each
  block reads the snapshot produced by the previous `CommitStateChanges` call.
- `--executor-workers`: parallel OCC workers inside each executor. The default
  is `min(12, GOMAXPROCS)`, following the `sei-v3` OCC worker default.
- `--prepare-workers`: parallel stateless preparation workers used for
  transaction RLP decode and sender recovery. The default is `GOMAXPROCS`.
- `--parse-workers`: parallel transaction decode/sender recovery workers inside
  each prepared block. The default `0` uses `1` when `--prepare-workers > 1` to
  avoid multiplying block-level and intra-block parser pools, otherwise it uses
  `GOMAXPROCS`.
- `--builders`: parallel block builders used to keep the input queue full. The
  default is `GOMAXPROCS`.
- `--queue-size`: buffered raw and prepared blocks. The default is `64`.
- `--result-pool-size`: reusable executor result slots. The default `0`
  sizes the pool for in-flight executor and async sink results; negative
  disables result pooling.
- `--result-sink`: executor output sink, either `discard` or `file`. The
  default is `discard`.
- `--persist-dir`: directory used by `--result-sink=file` for temporary
  append-only changeset and receipt files. Files are removed on shutdown or
  interrupt cleanup.
- `--persist-buffer-size`: buffered writer size for `--result-sink=file`.
- `--persist-queue-size`: async file-sink record queue size. The default `0`
  uses `2 * --queue-size`.
- `--metrics-addr`: Prometheus endpoint. The default is
  `127.0.0.1:9698`; set it to empty to disable HTTP metrics.
- `--report-interval`: stdout rate reporting interval. The default is `5s`.
- `--gas-price-wei`, `--min-gas-price-wei`, `--sender-balance-wei`,
  `--transfer-value-wei`: transaction economics for the generated accounts.
- `--recipient-conflict-rate`: fraction of each block's transactions that are
  paired onto shared recipients; `0` keeps recipients unique and `1` pairs all
  possible transactions.
- `--same-sender`: use one sender per native-transfer block and assign
  transaction nonces in block order.
- `--workload`: workload type, either `transfer`, `erc20-transfer`, or
  `snapshot-revert`.
- `--snapshot-revert-contract`, `--snapshot-revert-helper`: generated contract
  addresses used by `--workload=snapshot-revert`.

The command reports these saturation signals on stdout and at `/metrics`:

- block input throughput
- block preparation throughput
- prepared transactions per second
- block finishing throughput
- finished, successful, and failed transactions per second
- total gas consumed per second
- total OCC transaction rerun attempts
- prepared blocks queued for execution and cumulative totals
- result-sink records queued, enqueued, written, bytes written, enqueue wait,
  and write time
- result-pool capacity, available slots, and overflow allocations

Every run uses the Giga executor lifecycle:

- `generatedState` implements `evmonly.StateReader` and supplies immutable
  generated genesis balances, nonces, code, and storage.
- `evmonly.MemoryStore` opens versioned snapshots over that genesis state and
  applies the executor's encoded output through `CommitStateChanges`.
- `discardResultSink` discards the already-committed block result and receipts;
  it is not responsible for state persistence.

With `--result-sink=file`, after the in-memory Giga commit succeeds the loadtest
harness hands pooled `evmonly.BlockResult` values to an async writer through the
executor's `evmonly.ResultSink` interface. The writer appends changesets to
`changesets.rlp` and receipts to `receipts.rlp` under `--persist-dir`; each
record is framed as an 8-byte big-endian block height, an 8-byte big-endian RLP
payload length, and the RLP payload. The files are temporary calibration
artifacts and are removed when the process exits normally or handles
`SIGINT`/`SIGTERM`. `sink_enqueue_wait` is the primary backpressure signal: a
non-zero value means executor workers waited for async sink queue capacity.

Native and ERC20 transaction construction and genesis seeding come from
`github.com/sei-protocol/sei-load/generator/offline`. Future reusable EVM
workloads should add an offline scenario there and a block/recipient adapter in
this command. Executor-specific workloads can remain beside
`TransferWorkload`, `ERC20TransferWorkload`, and `SnapshotRevertWorkload` and
reuse the same block producer/prepare/executor/metrics pipeline.
