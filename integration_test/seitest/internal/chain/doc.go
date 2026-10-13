// Package chain stands up N sei-chain validators in a single Go process — real
// CometBFT consensus, no HTTP listener of any kind, and deterministic teardown.
// It is the engine under the seitest DSL, and internal/ is what keeps the DSL
// the only public contract.
//
// Use Validators = 1 or Validators >= 3; Start rejects 2. See validateTopology
// for why.
//
// # Usage
//
//	c, err := chain.Start(ctx, chain.Config{Validators: 4})
//	if err != nil { ... }
//	defer c.Close()
//	if err := c.WaitReady(ctx); err != nil { ... }
//	client := c.Validator(0).LocalClient()
//
// # Bring-up invariants
//
// Each is named, and referenced by name where the code establishes or asserts
// it. Every one that can break silently has a guard in invariants.go, called
// from startValidator — the single function every bring-up path passes through —
// rather than at the site that establishes it.
//
//   - EVM-serving-off: the app serves no EVM HTTP or WS listener. This is what
//     admits many networks per process: the evmrpc worker pool and metrics
//     printer are process-global sync.Once singletons that never cleanly
//     re-initialize, and they are reached only when a listener is constructed.
//     assertEVMServingOff reads the resolved config, not the engine's intent.
//   - no-listener bring-up: RPC.ListenAddress is empty. sei-tendermint gates its
//     whole RPC service on that field while building rpcEnv unconditionally, so
//     rpclocal.New works with nothing bound and a validator binds exactly one
//     port, for P2P. Config.ServeTendermintRPC is the recorded fallback.
//   - empty-valset: the genesis validator set is nil for N>=2, so each node
//     derives the valset from its own InitChain response; a pre-populated set
//     fails consensus replay. N=1 is the exception and pins its own validator —
//     see genesisDocFor.
//   - gentx-derived peer mesh: the engine never wires the mesh. Each gentx memo
//     carries nodeID@127.0.0.1:p2pPort, and genutil.GenAppStateFromConfig
//     mutates P2P.PersistentPeers in place on the very *config.Config the engine
//     holds and later hands to tmnode.New. Cloning that config before
//     collectGentxs, or starting validators before collecting, drops consensus
//     with no error — which is what assertPeerMesh exists to catch.
//   - consensus-params survival: genutil.ExportGenesisFileWithTime builds a
//     fresh DefaultConsensusParams and patches only Timeout.Commit, discarding
//     everything else. The engine does not call it;
//     assertGenesisConsensusParams checks the written file, and WaitReady
//     checks the live chain.
//   - metrics-off: Instrumentation.Prometheus stays false. Its listener binds a
//     fixed port, so a second validator in the process collides with the first.
//   - loopback bind scope: every listener is scoped to 127.0.0.1, which
//     tendermint's defaults are not, or the engine publishes externally
//     reachable consensus listeners.
//   - loopback conn-tracker ceiling: MaxIncomingConnectionAttempts is raised.
//     Loopback collapses every peer onto one IP, so the router's IP-keyed
//     conn-tracker counts the whole startup burst against a single key and
//     rejects peers once the per-IP cap trips. AllowDuplicateIP is a
//     peer-manager flag and does not touch the conn-tracker.
//
// # What teardown must not do
//
// Close never calls evmrpc.StopMetricsPrinter or closes the global EVM worker
// pool. Both are permanent process-wide sync.Once kills, so calling either is
// the one action that would reintroduce a one-network-per-process limit. With
// EVM serving off, neither is ever constructed and there is nothing to stop.
//
// # Coverage and cost
//
// The bring-up path is generic over N, but only N=1 is exercised by this
// package's tests today; N=3 and N=4 land with the multi-validator stage, and
// repeated Start/Close in one binary with the stage after it.
//
// N=1 reaches WaitReady in roughly 0.5-0.8s, of which Start is 0.4-0.7s
// (darwin/arm64, -race, tmpfs home). Blocks commit about every 50ms on the
// default timeouts. Treat a WaitReady an order of magnitude past that as a
// regression rather than a slow machine.
package chain
