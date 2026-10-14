# EVM-only RPC parity with v2

`TestV2Parity` (in `giga/evmonly/rpc/parity_test.go`) checks that the EVM-only
RPC answers like the v2 RPC (`evmrpc`), with v2 as the source of truth. It runs
in the ordinary Go test CI and needs no network, chain, or node.

A v2 node and an Autobahn node never serve the same chain, so the two RPCs are
compared through recorded fixtures rather than side by side. Each fixture in
`giga/evmonly/rpc/testdata/parity/` holds:

- the EVM state its blocks start from (`prestate`)
- each block's header as v2 reported it, the EVM block context v2 executed it
  under (coinbase, prevRandao, base fee, gas limit), and its transactions as raw
  Ethereum RLP
- a generated set of requests (blocks, transactions, receipts, `eth_getLogs`
  filters, latest state, misses, fee history, and any calls the fixture adds)
  with v2's response to each

The test executes each fixture's blocks with the EVM-only executor from the
prestate, serves the result through the EVM-only RPC handler, sends every
request, and compares each response with v2's field by field.

## Known differences

`known_diffs.json` lists the differences the test accepts, each with the reason
it exists. An entry matches on method and response path, and can be narrowed by
request parameters, by text in either side's value, and by fixture. The list is
a ratchet: a difference it does not cover fails the test, and so does an entry
that no fixture produces any more. Fixing a divergence therefore means deleting
its entry in the same change.

Add an entry only for a difference that is intended or not yet scheduled, and
narrow it as far as the evidence allows: an entry broad enough to cover a real
regression hides it.

## Recording fixtures

Fixtures are recorded locally and reviewed like code. Both recorders write to
`giga/evmonly/rpc/testdata/parity/`.

### Synthetic

`v2record` runs scenarios on an in-process v2 node and records them:

```bash
EVMONLY_PARITY_RECORD=1 go test ./giga/evmonly/rpc/parity/v2record/ -run TestRecordSyntheticFixtures -count=1
```

It writes `synthetic-basic.json`, and `captured-synthetic-basic.json`, which
captures two mid-chain blocks of the same chain through the `capture` package to
exercise the path mainnet fixtures take. Recording is deterministic, so an
unchanged v2 rewrites identical files. To cover new behaviour, add transactions
or calls to a scenario in `synthetic_test.go`, or add a scenario.

### Mainnet

`paritycapture` captures consecutive blocks from a v2 archive node. The node
must serve `debug_traceBlockByNumber` with the `prestateTracer`, which the
prestate is built from, and the same node's Tendermint RPC supplies each
block's exact time.

```bash
go run ./giga/evmonly/rpc/parity/cmd/paritycapture \
  -evm-rpc http://<archive>:8545 -tendermint-rpc http://<archive>:26657 \
  -block <height> -count 1 -name pacific-1-<height>
```

The capture refuses a block holding a Cosmos transaction, whose state changes
the fixture cannot replay, and a block that calls a Sei custom precompile, which
the EVM-only executor rejects. Prefer blocks that add coverage the existing
fixtures lack: contract interactions with many logs, deployments, reverts, and
a mix of transaction types. Keep the set small; every fixture is replayed on
every CI run.

## Scope

The replay feeds the executor the context v2 executed each block under and v2's
chain configuration, so a difference reflects the EVM-only executor and RPC, not
a chain-configuration choice. A production EVM-only node differs from that
context by design: it executes at a zero base fee with a zero coinbase, under
`params.AllDevChainProtocolChanges`. The replay's backend stands in for the
node's (`sei-tendermint/internal/evmonlyapp` behind `rpc.Backend`), so that
adapter is not covered here.
