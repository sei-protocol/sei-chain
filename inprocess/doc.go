//go:build inprocess

// Package inprocess stands up N sei-chain validators in a single Go process,
// reaching real CometBFT consensus and each serving its own RPC stack
// (Tendermint RPC + EVM JSON-RPC HTTP/WS), with deterministic teardown.
//
// It is the in-process provisioning foundation for the SDK "local" provider
// (design: bdchatham-designs/designs/test-harness/sdk-local-provider-lld.md).
// The package is gated behind the `inprocess` build tag so its heavy
// sei-tendermint/sei-cosmos bring-up never leaks into a normal `seid` build.
//
// # Usage
//
//	net, err := inprocess.Start(ctx, inprocess.Options{Validators: 4})
//	if err != nil { ... }
//	defer net.Close()
//	if err := net.WaitReady(ctx); err != nil { ... }
//	rpc := net.Node(0).TendermintRPC() // http://127.0.0.1:PORT
//
// # Why a native API, not the SDK sei.Provider interface
//
// The LLD's target is for Start to back the SDK's sei.Provider so suites written
// against sei.Open(ctx, "local") run unchanged. That wiring is deferred: the SDK
// lives in the github.com/sei-protocol/sei-k8s-controller module, which declares
// `go >= 1.26.0`; sei-chain runs go 1.25.6, so importing the SDK forces a
// chain-wide toolchain bump (and pulls the controller's controller-runtime/AWS
// dep graph into the seid build). The handle methods here intentionally mirror
// sei.NodeHandle / sei.NetworkHandle so a thin adapter can satisfy the SDK
// interface once the toolchain skew is resolved — see Node and Network below.
//
// # Recipe (the gotchas that make N>1 consensus + per-node RPC work)
//
// These are the load-bearing deltas vs sei-cosmos/testutil/network.New, proven
// by the N-RPC spike and preserved here:
//
//  1. genDoc.Validators = nil — let CometBFT derive the valset from the app's
//     InitChain response. testutil/network sets it to []{self}, which fails
//     consensus replay for N>1.
//  2. Full P2P mesh — persistent-peers wired nodeID@127.0.0.1:p2pPort across all
//     N (testutil/network wires zero) — without the mesh nodes never gossip and
//     consensus never forms for N>1.
//  3. Injected AppOptions enable EVM HTTP/WS on per-node ports — without them
//     app.TestAppOpts hard-disables the listeners and no node serves EVM.
//  4. tmCfg.Instrumentation.Prometheus = false — metrics off avoids the
//     dup-registry panic from the process-wide registries. Invariant: metrics
//     must stay off until the evmrpc/EVM-keeper metrics are de-globalized —
//     re-enabling Prometheus without that reintroduces the panic.
//  5. TM RPC / P2P listeners scoped to 127.0.0.1 (they default to [::] /
//     0.0.0.0) — without scoping an in-process harness publishes externally
//     reachable consensus/RPC listeners. Caveat (accepted): the EVM HTTP/WS listeners bind all
//     interfaces (0.0.0.0) for the harness lifetime; only TM RPC/P2P are
//     loopback-scoped. They run on free ephemeral ports, dialed via 127.0.0.1.
//     Tightening requires a bind-host option in evmrpc (not yet present).
//  6. MaxIncomingConnectionAttempts raised — loopback collapses all peers onto
//     127.0.0.1, so the router's IP-keyed conn-tracker counts the startup burst
//     on one key — without the raise the burst trips the per-IP cap and peers
//     are rejected.
package inprocess
