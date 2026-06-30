# evmonly-loadtest

`evmonly-loadtest` is a standalone executable for feeding synthetic blocks to
the EVM-only executor without Cosmos SDK state, mempool, RPC, or persistence.

It currently generates pure EVM legacy transfer transactions. Each generated
sender account has exactly one nonce-0 transaction and is funded in the
command's in-memory genesis state before its block is queued. Recipients are
unique by default so the transfer workload can be extended toward
less-conflicting account layouts; pass `--recipient=0x...` to force a single
recipient.

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
before starting executor workers:

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

The zero gas price/min-gas settings keep the transfer workload focused on the
optimistic no-overlap case. Non-zero fees make every transaction update the
same coinbase balance, which is a real intra-block conflict.

Useful knobs:

- `--workers`: parallel executor workers. The default is `1`.
- `--executor-workers`: parallel OCC workers inside each executor. The default
  is `min(12, GOMAXPROCS)`, following the `sei-v3` OCC worker default.
- `--builders`: parallel block builders used to keep the input queue full. The
  default is `GOMAXPROCS`.
- `--queue-size`: buffered blocks ready for workers. The default is `64`.
- `--target-blocks-per-sec`: cap block input rate. The default `0` feeds as
  fast as block generation and the queue allow.
- `--prebuild-blocks`: generate all bounded blocks before starting executor
  workers. This separates build throughput from executor throughput.
- `--metrics-addr`: Prometheus endpoint. The default is
  `127.0.0.1:9698`; set it to empty to disable HTTP metrics.
- `--report-interval`: stdout rate reporting interval. The default is `5s`.
- `--gas-price-wei`, `--min-gas-price-wei`, `--sender-balance-wei`,
  `--transfer-value-wei`: transaction economics for the generated accounts.

The command reports these saturation signals on stdout and at `/metrics`:

- block input throughput
- block finishing throughput
- finished transactions per second
- total gas consumed per second
- queued blocks and cumulative totals

The executor output is intentionally discarded through mocks:

- `generatedState` implements `evmonly.StateReader` and supplies generated
  genesis balances, nonces, code, and storage.
- `discardStateWriter` implements `evmonly.StateWriter` and sinks the
  executor `StateChangeSet`.
- `discardReceiptSink` sinks Ethereum receipts.

Future workloads should add another workload builder beside `transferWorkload`.
ERC20 transfers can reuse the same harness by adding contract code/storage to
`generatedState`, generating calldata transactions, and keeping the same block
producer/executor/metrics pipeline.
