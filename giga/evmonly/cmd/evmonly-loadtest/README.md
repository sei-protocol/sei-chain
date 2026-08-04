# evmonly-loadtest

`evmonly-loadtest` is a standalone executable for feeding synthetic blocks to
the EVM-only executor without Cosmos SDK state, mempool, RPC, or chain
persistence.

It currently generates pure EVM legacy transfer transactions, ERC20 transfer
transactions, and a contract-call workload that exercises nested StateDB
snapshot/revert behavior. By default, each generated sender account has one
nonce-0 transaction and is funded in the command's in-memory genesis state
before its block is queued. Recipients are unique by default so the transfer
workloads exercise the optimistic no-overlap case. Pass
`--recipient-conflict-rate=<0..1>` to pair that fraction of each block's
transactions onto shared recipients, or pass `--recipient=0x...` to force all
transactions to a single recipient. Pass `--same-sender` to use one sender per
block with sequential transaction nonces.

Run a bounded test:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest --blocks=1000 --txs-per-block=1000
```

Run continuously until interrupted:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest --txs-per-block=1000
```

Continuous mode with unique generated accounts is memory-limited: the in-memory
genesis state retains every funded sender account, and ERC20 runs also retain a
storage slot for every generated token holder. Use bounded `--blocks` runs when
comparing throughput, especially for long ERC20 or conflict-free transfer runs.

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
  --gas-price-wei=0 --min-gas-price-wei=0

# Hot recipient: unique senders all credit one account.
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --gas-price-wei=0 --min-gas-price-wei=0 \
  --recipient=0x00000000000000000000000000000000000000f1

# Same-sender nonce chain: one sender and sequential nonces within each block.
go run ./giga/evmonly/cmd/evmonly-loadtest \
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

To isolate executor throughput from block generation, prebuild a bounded run
before starting the prepare/recover and executor workers:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest \
  --metrics-addr= \
  --report-interval=5s \
  --prebuild-blocks \
  --blocks=400 \
  --txs-per-block=5000 \
  --builders=48 \
  --workers=1 \
  --executor-workers=24 \
  --gas-price-wei=0 \
  --min-gas-price-wei=0 \
  --queue-size=512
```

Prebuilding requires `--blocks > 0` and stores every raw block in memory.
Sender recovery still runs in the measured phase, but it is pipelined ahead of
execution through `--prepare-workers`.

The zero gas price/min-gas settings keep the conflict-free transfer workload
focused on the optimistic no-overlap case. Non-zero fees make every transaction
update the same coinbase balance, which is a real intra-block conflict.

Useful knobs:

- `--workers`: parallel executor workers. The default is `1`. Prepared blocks
  are forwarded to workers in block-number order, but `--workers > 1` can still
  finish execution out of order; this is safe for the harness because generated
  state is frozen for prebuilt runs and executor changesets are not applied back
  into the input state.
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
- `--target-blocks-per-sec`: cap block input rate. The default `0` feeds as
  fast as block generation and the queue allow.
- `--prebuild-blocks`: generate all bounded blocks before starting executor
  workers. This separates build throughput from executor throughput.
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
- finished transactions per second
- total gas consumed per second
- total OCC transaction rerun attempts
- prepared blocks queued for execution and cumulative totals
- result-sink records queued, enqueued, written, bytes written, enqueue wait,
  and write time
- result-pool capacity, available slots, and overflow allocations

The default executor output path intentionally discards results through mocks:

- `generatedState` implements `evmonly.StateReader` and supplies generated
  genesis balances, nonces, code, and storage.
- `discardResultSink` applies the executor `StateChangeSet` to
  `discardStateWriter` and discards Ethereum receipts.

With `--result-sink=file`, the loadtest harness hands pooled
`evmonly.BlockResult` values to an async writer through the executor's
`evmonly.ResultSink` interface. The writer appends changesets to
`changesets.rlp` and receipts to `receipts.rlp` under `--persist-dir`; each
record is framed as an 8-byte big-endian block height, an 8-byte big-endian RLP
payload length, and the RLP payload. The files are temporary calibration
artifacts and are removed when the process exits normally or handles
`SIGINT`/`SIGTERM`. `sink_enqueue_wait` is the primary backpressure signal: a
non-zero value means executor workers waited for async sink queue capacity.

Future workloads should add another workload builder beside `transferWorkload`,
`erc20TransferWorkload`, and `snapshotRevertWorkload`, then reuse the same
block producer/prepare/executor/metrics pipeline.
