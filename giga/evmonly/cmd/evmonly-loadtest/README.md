# evmonly-loadtest

`evmonly-loadtest` is a standalone executable for feeding synthetic blocks to
the EVM-only executor without Cosmos SDK state, mempool, RPC, or persistence.

It currently generates pure EVM legacy transfer transactions and ERC20 transfer
transactions. Each generated sender account has exactly one nonce-0 transaction
and is funded in the command's in-memory genesis state before its block is
queued. Recipients are unique by default so the workloads exercise the optimistic
no-overlap case; pass `--recipient=0x...` to force a single recipient.

Run a bounded test:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest --blocks=1000 --txs-per-block=1000
```

Run continuously until interrupted:

```bash
go run ./giga/evmonly/cmd/evmonly-loadtest --txs-per-block=1000
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

The zero gas price/min-gas settings keep the transfer workload focused on the
optimistic no-overlap case. Non-zero fees make every transaction update the
same coinbase balance, which is a real intra-block conflict.

Useful knobs:

- `--workers`: parallel executor workers. The default is `1`.
- `--executor-workers`: parallel OCC workers inside each executor. The default
  is `min(12, GOMAXPROCS)`, following the `sei-v3` OCC worker default.
- `--prepare-workers`: parallel stateless preparation workers used for
  transaction RLP decode and sender recovery. The default is `GOMAXPROCS`.
- `--pin-workers`: on Linux, lock prepare and OCC workers to OS threads and
  pin them to CPUs. The default is disabled.
- `--prepare-cpu-offset`: first CPU used by pinned prepare workers. The default
  is `0`.
- `--executor-cpu-offset`: first CPU used by pinned OCC workers. The default
  `-1` starts after the prepare-worker range.
- `--builders`: parallel block builders used to keep the input queue full. The
  default is `GOMAXPROCS`.
- `--queue-size`: buffered raw and prepared blocks. The default is `64`.
- `--target-blocks-per-sec`: cap block input rate. The default `0` feeds as
  fast as block generation and the queue allow.
- `--prebuild-blocks`: generate all bounded blocks before starting executor
  workers. This separates build throughput from executor throughput.
- `--metrics-addr`: Prometheus endpoint. The default is
  `127.0.0.1:9698`; set it to empty to disable HTTP metrics.
- `--report-interval`: stdout rate reporting interval. The default is `5s`.
- `--gas-price-wei`, `--min-gas-price-wei`, `--sender-balance-wei`,
  `--transfer-value-wei`: transaction economics for the generated accounts.
- `--workload`: workload type, either `transfer` or `erc20-transfer`.

The command reports these saturation signals on stdout and at `/metrics`:

- block input throughput
- block preparation throughput
- prepared transactions per second
- block finishing throughput
- finished transactions per second
- total gas consumed per second
- prepared blocks queued for execution and cumulative totals

The executor output is intentionally discarded through mocks:

- `generatedState` implements `evmonly.StateReader` and supplies generated
  genesis balances, nonces, code, and storage.
- `discardStateWriter` implements `evmonly.StateWriter` and sinks the
  executor `StateChangeSet`.
- `discardReceiptSink` sinks Ethereum receipts.

Future workloads should add another workload builder beside `transferWorkload`
and `erc20TransferWorkload`, then reuse the same block
producer/prepare/executor/metrics pipeline.
