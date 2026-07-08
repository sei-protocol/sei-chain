//go:build inprocess

package inprocess

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
)

// probeClient is the default HTTP client the readiness probes dial with. It is a
// package-level default so WaitReady takes only a ctx (mirroring the SDK's
// sei.NodeHandle.WaitReady), keeping the http client an internal detail. The
// short timeout bounds a single /status or eth_blockNumber probe — the overall
// wait is governed by the caller's ctx, not this.
var probeClient = &http.Client{Timeout: 5 * time.Second}

// Node is a handle to one running in-process validator. Its method set mirrors
// the SDK's sei.NodeHandle (EVMRPC/TendermintRPC/REST/WaitReady/Object) so a thin
// adapter can satisfy that interface once the SDK toolchain skew is resolved
// (see doc.go). Endpoint getters return loopback URLs that are valid as soon as
// the node is started; WaitReady gates on the listeners actually serving.
type Node struct{ n *node }

// Name is the node's moniker (node0, node1, ...).
func (h Node) Name() string { return h.n.moniker }

// Namespace is "" for in-process nodes (no k8s namespace); present for SDK
// handle parity.
func (h Node) Namespace() string { return "" }

// Home is the node's on-disk home dir (the seid --home target). It holds the
// node's config/, data/, and the `test` keyring this node's genesis keys were
// written into — what the YAML runner's in-process arm points a host `seid` at.
// Not part of the SDK NodeHandle surface (a home dir is in-process-only); exposed
// because the host-binary runner arm needs it.
func (h Node) Home() string { return h.n.home }

// RPCNodeAddr is the node's CometBFT RPC dial address in tcp:// form
// (tcp://127.0.0.1:PORT) — the value a host `seid --node` flag wants, distinct
// from TendermintRPC's http:// form used by the readiness probes.
func (h Node) RPCNodeAddr() string { return h.n.rpcAddr }

// TendermintRPC is the node's CometBFT RPC base URL (http://127.0.0.1:PORT).
func (h Node) TendermintRPC() string { return "http://" + stripScheme(h.n.rpcAddr) }

// EVMRPC is the node's EVM JSON-RPC HTTP URL. The URL dials loopback, but the
// listener itself binds 0.0.0.0 (see doc.go's 0.0.0.0 EVM caveat).
func (h Node) EVMRPC() string { return fmt.Sprintf("http://127.0.0.1:%d", h.n.httpPort) }

// EVMWS is the node's EVM JSON-RPC WebSocket URL. Not part of the SDK
// NodeHandle surface, but the in-process harness binds it, so it is exposed.
// The URL dials loopback, but the listener itself binds 0.0.0.0 (see doc.go's
// 0.0.0.0 EVM caveat).
func (h Node) EVMWS() string { return fmt.Sprintf("ws://127.0.0.1:%d", h.n.wsPort) }

// REST is "" — the harness does not start the Cosmos LCD listener (reserved:
// REST is part of the SDK NodeHandle shape, so it is present as an honest parity
// stub; validators serve none by default).
func (h Node) REST() string { return "" }

// Object returns the live *node.Node behind the handle (SDK escape hatch: the
// dynamic value behind any). Read-oriented — driving it is an in-process-only
// capability k8s mode never offers.
func (h Node) Object() any { return h.n.tmNode }

// GigaExecutorEnabled reports whether this running node selected the Giga EVM
// execution path — the resolved app.GigaExecutorEnabled that DeliverTx branches on
// (app/app.go). It is the post-boot downgrade guard for a pinned-giga network:
// the value is read from the running app, not the requested config, so a flipped
// gigaconfig.DefaultConfig.Enabled or a renamed flag const surfaces here as false
// and a test can fail loud instead of silently exercising the V2 path. (The best-
// effort evmone dlopen is not part of this signal — evmone is staged on the keeper
// but does not gate path selection, so a host missing the pinned library still runs
// the giga executor.)
func (h Node) GigaExecutorEnabled() bool { return h.n.app.GigaExecutorEnabled }

// GigaOCCEnabled reports whether OCC parallel execution is active on the giga path
// (app.GigaOCCEnabled). Paired with GigaExecutorEnabled it catches an OCC-off
// downgrade that would drop the pinned-giga net to sequential giga execution.
func (h Node) GigaOCCEnabled() bool { return h.n.app.GigaOCCEnabled }

// WaitReady blocks until this node has joined consensus (height advancing) and
// its EVM listener is serving, or ctx fires. Its single-ctx signature mirrors
// the SDK's sei.NodeHandle.WaitReady; the probe HTTP client is an internal
// default (probeClient).
func (h Node) WaitReady(ctx context.Context) error {
	if err := waitHeightAdvances(ctx, probeClient, h.TendermintRPC(), 1); err != nil {
		return fmt.Errorf("%s tendermint: %w", h.n.moniker, err)
	}
	if err := waitEVMServing(ctx, probeClient, h.EVMRPC()); err != nil {
		return fmt.Errorf("%s evm: %w", h.n.moniker, err)
	}
	return nil
}

// Node returns a handle to the i-th validator (0-based). It panics on an
// out-of-range index — a programming error, not a runtime condition.
func (net *Network) Node(i int) Node { return Node{n: net.nodes[i]} }

// Nodes returns handles to every validator in index order.
func (net *Network) Nodes() []Node {
	out := make([]Node, len(net.nodes))
	for i := range net.nodes {
		out[i] = Node{n: net.nodes[i]}
	}
	return out
}

// Len is the validator count.
func (net *Network) Len() int { return len(net.nodes) }

// WaitReady blocks until every node has joined consensus and is serving EVM, or
// ctx fires. It is the heavy readiness gate (per-node height-advance + EVM
// probe), distinct from Start (which only constructs + starts the nodes).
func (net *Network) WaitReady(ctx context.Context) error {
	for i := range net.nodes {
		if err := net.Node(i).WaitReady(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Close tears every node down deterministically and is idempotent. Order:
// stop each tendermint node (halts consensus + block production), stop each
// EVM HTTP/WS listener, drain the EVM worker pool, then remove the temp dir the
// harness owns. Safe to call from a defer on both the success and partial-start
// paths; nodes that never started are skipped.
func (net *Network) Close() {
	if net.closed {
		return
	}
	net.closed = true

	for _, n := range net.nodes {
		stopNode(n)
	}
	// The EVM worker pool (evmrpc.GetGlobalWorkerPool) and the metrics printer
	// (evmrpc.StopMetricsPrinter, a sync.Once) are process-wide singletons, NOT
	// Network-owned. The pool is deliberately not Closed here: Close is permanent
	// (the Once never re-fires), so a second Start would inherit a dead pool — which
	// is why Start refuses one (see networkStarted). Their goroutines are reaped at
	// process exit; de-globalizing these in evmrpc is the proper fix for repeated
	// Start/Close in one process.

	if net.ownBaseDir && net.baseDir != "" {
		_ = os.RemoveAll(net.baseDir)
	}
}

// stopNode shuts one node's tendermint service and EVM listeners. Each step is
// guarded so a nil (never-started) field on a partial-start path is a no-op.
//
// Caveat: an EVM serve goroutine parks on the node's first-block start signal
// (RegisterLocalServices), which only fires once the node commits a block. If a
// node is stopped before that — a partial start or a WaitReady timeout — the
// listener never binds, its Stop is a no-op, and the parked goroutine (with its
// app references) lives until process exit. Bounded under one network per process;
// it compounds that limit rather than breaking teardown.
func stopNode(n *node) {
	if n.tmNode != nil && n.tmNode.IsRunning() {
		n.tmNode.Stop()
		n.tmNode.Wait()
	}
	if n.app != nil {
		stopEVMServer(n.app.EVMHTTPServer())
		stopEVMServer(n.app.EVMWebSocketServer())
	}
}

// stopEVMServer stops an EVM listener if it was constructed (nil when the
// listener was disabled).
func stopEVMServer(s evmrpc.EVMServer) {
	if s != nil {
		s.Stop()
	}
}

// stripScheme drops a leading scheme:// from a listen address so it can be
// recomposed with a concrete scheme (TM RPC config carries tcp://).
func stripScheme(addr string) string {
	if rest, ok := strings.CutPrefix(addr, "tcp://"); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(addr, "http://"); ok {
		return rest
	}
	return addr
}
