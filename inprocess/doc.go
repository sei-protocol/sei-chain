//go:build inprocess

// Package inprocess stands up N sei-chain validators in a single Go process —
// real CometBFT consensus, each node serving its own Tendermint RPC + EVM
// JSON-RPC (HTTP/WS), with deterministic teardown. It is the in-process
// provisioning foundation for the SDK "local" provider (design:
// bdchatham-designs/designs/test-harness/sdk-local-provider-lld.md).
//
// Use Validators = 1 or Validators >= 3; Start rejects 2 (see "Choosing the
// validator count"). The package is gated behind the `inprocess` build tag so
// its heavy sei-tendermint/sei-cosmos bring-up never leaks into a normal `seid`
// build.
//
// # Usage
//
//	net, err := inprocess.Start(ctx, inprocess.Options{Validators: 4})
//	if err != nil { ... }
//	defer net.Close()
//	if err := net.WaitReady(ctx); err != nil { ... }
//	rpc := net.Node(0).TendermintRPC() // http://127.0.0.1:PORT
//
// # Choosing the validator count
//
// Pick 1 or >= 3 — never 2. The constraint is CometBFT's block-sync→consensus
// handoff, not a voting-power quorum:
//
//   - N=1: the sole validator skips block-sync and proposes blocks solo
//     (sei-tendermint onlyValidatorIsUs in node/setup.go gates
//     `blockSync := !onlyValidatorIsUs` in node/node.go). That decision reads
//     the genesis-derived valset before InitChain, so the harness pins the
//     single validator into genesis for N=1 — an empty valset would leave size
//     0, defeat onlyValidatorIsUs, and hang the solo node in block-sync (see
//     startNode).
//   - N=2 deadlocks: each node has exactly one peer, but BlockPool.IsCaughtUp
//     (internal/blocksync/pool.go) requires len(peers) > 1 to ever report
//     caught-up, so neither node leaves block-sync. It is a peer-count
//     deadlock, not a stake threshold — Start rejects 2 loudly rather than hang.
//   - N>=3: every node has >= 2 peers, so IsCaughtUp can fire and hand off to
//     consensus. N=3 is the smallest real multi-node topology.
//
// # Bring-up invariants
//
// These are the load-bearing deltas vs sei-cosmos/testutil/network.New. Each is
// named and referenced by name at its point of use in the code — there is no
// central numbered list to drift:
//
//   - empty-valset: set genDoc.Validators = nil and let CometBFT derive the
//     valset from the app's InitChain response. testutil/network sets it to
//     []{self}, which fails consensus replay for N>1. (N=1 is the exception —
//     it pins the validator into genesis; see "Choosing the validator count".)
//   - gentx-derived peer mesh: the harness never wires the P2P mesh. Each
//     validator's gentx memo carries nodeID@127.0.0.1:p2pPort, and
//     collectGentxs → genutil.GenAppStateFromConfig (sei-cosmos x/genutil)
//     mutates P2P.PersistentPeers in place on the same *config.Config the
//     harness holds in node.tmCfg and later hands to tmnode.New. Without it
//     nodes never gossip and consensus never forms for N>1. The in-place
//     mutation is invisible at the harness layer and fragile — cloning tmCfg
//     before collectGentxs, or building nodes before collecting, silently
//     breaks consensus for all N — so Start asserts PersistentPeers is
//     non-empty (N>=2) right after collectGentxs and fails loudly otherwise.
//   - EVM-enable injection: injected AppOptions enable EVM HTTP/WS on per-node
//     ports. Without them app.TestAppOpts hard-disables the listeners and no
//     node serves EVM.
//   - metrics-off: set tmCfg.Instrumentation.Prometheus = false to avoid the
//     dup-registry panic from the process-wide registries. Metrics must stay
//     off until the evmrpc/EVM-keeper metrics are de-globalized — re-enabling
//     Prometheus before then reintroduces the panic.
//   - loopback bind scope: scope TM RPC and P2P to 127.0.0.1 (they default to
//     [::]/0.0.0.0), or the harness publishes externally reachable
//     consensus/RPC listeners. The EVM HTTP/WS listeners are the accepted
//     exception: they bind all interfaces (0.0.0.0) because evmrpc has no
//     bind-host option yet, but run on free ephemeral ports dialed via
//     127.0.0.1. A rare port-bind collision — the free port is taken between
//     freePort's probe-close (net.Listen on 127.0.0.1:0) and the listener's bind —
//     panics the node's serve goroutine (the production fail-loud path,
//     intentionally not diverted). Setting SEI_INPROCESS_PORT_BASE switches
//     freePort to deterministic per-process allocation (base + offset, no probe),
//     which removes this TOCTOU across cooperating processes (CI shards); see
//     freePort. If it ever flakes without that, harden the probe-to-bind window
//     rather than re-add a serve-error diversion.
//   - loopback conn-tracker ceiling: raise MaxIncomingConnectionAttempts.
//     Loopback collapses every peer onto 127.0.0.1, so the router's IP-keyed
//     conn-tracker counts the whole startup burst against one key; without the
//     raise the burst trips the per-IP cap and peers are rejected.
//
// # Why a native API, not the SDK sei.Provider interface
//
// The LLD's eventual target is for Start to back the SDK's sei.Provider so
// suites written against sei.Open(ctx, "local") run unchanged. That wiring is
// deferred: the SDK lives in the github.com/sei-protocol/sei-k8s-controller
// module, which declares `go >= 1.26.0`, while sei-chain runs go 1.25.6 — so
// importing it would force a chain-wide toolchain bump and pull the controller's
// controller-runtime/AWS dep graph into the seid build. The handle methods here
// intentionally mirror sei.NodeHandle / sei.NetworkHandle so a thin adapter can
// satisfy the SDK interface once the skew is resolved — see Node and Network.
package inprocess
