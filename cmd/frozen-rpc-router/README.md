# Frozen RPC router

`frozen-rpc-router` exposes an HTTP EVM JSON-RPC endpoint backed by a live node
and any number of nodes running with `freeze-height`.

A freeze height is an exclusive boundary. A node started with
`freeze-height = 100` serves blocks through height 99, so the router sends
height 99 to that node and height 100 to the next configured interval (or the
live node).

```sh
go run ./cmd/frozen-rpc-router \
  --listen-address 0.0.0.0:8545 \
  --live-node localhost:9545 \
  --frozen-node 1000000=localhost:9546 \
  --frozen-node 2000000=10.0.0.12:8545
```

Repeat `--frozen-node` for every `freeze-height=ip:port` pair. HTTP and HTTPS
URLs are also accepted. Frozen nodes may be listed in any order, but freeze
heights must be unique.

Methods with explicit numeric block parameters are routed to the matching
interval. `eth_getLogs` and `eth_feeHistory` are routed only when their entire
explicit range belongs to one interval; ranges crossing an interval boundary
return JSON-RPC error `-32000`. Latest-style block tags, methods without block
numbers, stateful filter methods, subscriptions, and WebSocket connections use
the live node.

Single-backend HTTP responses include `Sei-RPC-Route: frozen:<height>` or
`Sei-RPC-Route: live`. A batch split across backends returns
`Sei-RPC-Route: mixed`.
