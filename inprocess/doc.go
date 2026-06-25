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
// # Invariants (the gotchas that make N>1 consensus + per-node RPC work)
//
// These are the load-bearing deltas vs sei-cosmos/testutil/network.New, proven
// by the N-RPC spike and preserved here. Each is named and referenced by name at
// its point-of-use in the code (there is no central numbered list to drift):
//
//   - empty-valset invariant: genDoc.Validators = nil — let CometBFT derive the
//     valset from the app's InitChain response. testutil/network sets it to
//     []{self}, which fails consensus replay for N>1. (N=1 is the documented
//     exception under the validator-count rule below.)
//   - gentx-derived peer mesh: the P2P mesh is NOT wired explicitly by the
//     harness. Each validator's gentx memo carries nodeID@127.0.0.1:p2pPort, and
//     collectGentxs → genutil.GenAppStateFromConfig (sei-cosmos x/genutil) mutates
//     P2P.PersistentPeers IN PLACE on the same *config.Config the harness holds in
//     node.tmCfg and later hands to tmnode.New. So the mesh is derived from the
//     gentxs, not set by harness code — without it nodes never gossip and
//     consensus never forms for N>1. This in-place mutation is invisible at the
//     harness layer and fragile: a refactor that clones tmCfg before collectGentxs,
//     or builds nodes before collecting, silently breaks consensus for all N. Start
//     guards it — after collectGentxs it asserts PersistentPeers is non-empty for
//     N>=2 and fails loudly otherwise.
//   - EVM-enable injection: injected AppOptions enable EVM HTTP/WS on per-node
//     ports — without them app.TestAppOpts hard-disables the listeners and no node
//     serves EVM.
//   - metrics-off constraint: tmCfg.Instrumentation.Prometheus = false — metrics
//     off avoids the dup-registry panic from the process-wide registries. Metrics
//     must stay off until the evmrpc/EVM-keeper metrics are de-globalized —
//     re-enabling Prometheus without that reintroduces the panic.
//   - loopback bind scope: TM RPC / P2P listeners scoped to 127.0.0.1 (they
//     default to [::] / 0.0.0.0) — without scoping an in-process harness publishes
//     externally reachable consensus/RPC listeners. 0.0.0.0 EVM caveat (accepted):
//     the EVM HTTP/WS listeners bind all interfaces (0.0.0.0) for the harness
//     lifetime; only TM RPC/P2P are loopback-scoped. They run on free ephemeral
//     ports, dialed via 127.0.0.1. Tightening requires a bind-host option in
//     evmrpc (not yet present). A rare EVM port-bind collision (the free port is
//     taken between FreeTCPAddr's probe-close and the listener's bind) panics the
//     node's serve goroutine — the production fail-loud path, intentionally not
//     diverted here. If it ever flakes, the fix is hardening the FreeTCPAddr
//     bind-close-rebind TOCTOU window, NOT re-adding a serve-error diversion.
//   - loopback conn-tracker ceiling: MaxIncomingConnectionAttempts raised —
//     loopback collapses all peers onto 127.0.0.1, so the router's IP-keyed
//     conn-tracker counts the startup burst on one key — without the raise the
//     burst trips the per-IP cap and peers are rejected.
//
// # Validator-count rule: 1 or >= 3 (2 is the trap)
//
// When wiring a suite, pick Validators = 1 or Validators >= 3. Start rejects 2.
// The constraint is CometBFT's block-sync→consensus handoff, NOT a voting-power
// quorum:
//
//   - N=1 works. A sole validator skips block-sync and proposes blocks solo
//     (sei-tendermint onlyValidatorIsUs, node/setup.go, gating
//     `blockSync := !onlyValidatorIsUs` in node/node.go). That decision reads the
//     genesis-derived valset BEFORE InitChain, so the harness pins the single
//     validator into genesis for N=1 (the empty-valset invariant would leave size
//     0, defeat onlyValidatorIsUs, and the solo node would hang in block-sync — see
//     startNode).
//   - N=2 hangs. Each node has exactly one peer, and BlockPool.IsCaughtUp
//     (internal/blocksync/pool.go) hard-requires len(peers) > 1 to ever report
//     caught-up, so neither node leaves block-sync. This is a peer-count deadlock,
//     not a stake threshold. Start rejects N=2 loudly rather than let it hang.
//   - N>=3 works. Every node has >= 2 peers, so IsCaughtUp can fire and hand off
//     to consensus. N=3 is the smallest real multi-node topology.
package inprocess
