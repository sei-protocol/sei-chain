//go:build inprocess

package inprocess

import (
	"context"
	"fmt"
	"net/http"
	"os"
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

// TendermintRPC is the node's CometBFT RPC base URL (http://127.0.0.1:PORT).
func (h Node) TendermintRPC() string { return "http://" + stripScheme(h.n.rpcAddr) }

// EVMRPC is the node's EVM JSON-RPC HTTP URL.
func (h Node) EVMRPC() string { return fmt.Sprintf("http://127.0.0.1:%d", h.n.httpPort) }

// EVMWS is the node's EVM JSON-RPC WebSocket URL. Not part of the SDK
// NodeHandle surface, but the in-process harness binds it, so it is exposed.
func (h Node) EVMWS() string { return fmt.Sprintf("ws://127.0.0.1:%d", h.n.wsPort) }

// REST is "" — the harness does not enable the Cosmos LCD listener (validators
// serve none by default; present for SDK handle parity).
func (h Node) REST() string { return "" }

// GRPC is the node's Cosmos gRPC address (host:port). Not in the SDK NodeHandle
// surface (gRPC is not a published status endpoint); exposed for in-process dials.
func (h Node) GRPC() string { return h.n.grpcAddr }

// Object returns the live *node.Node behind the handle (SDK escape hatch: the
// dynamic value behind any). Read-oriented — driving it is an in-process-only
// capability k8s mode never offers.
func (h Node) Object() any { return h.n.tmNode }

// ServeErr returns the channel EVM listener Start() failures are reported on
// (instead of the process-wide panic the production path uses). A non-nil
// receive means that node's EVM listener failed to bind; consensus may still be
// healthy. Buffered (cap 2: HTTP + WS).
func (h Node) ServeErr() <-chan error { return h.n.serveErr }

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
	// The EVM worker pool (evmrpc.GetGlobalWorkerPool) is a process-wide
	// sync.Once singleton, NOT Network-owned. Deliberately not Closed here:
	// Close is permanent (the Once never re-fires), so a second Start in the
	// same process would inherit a closed pool and every EVM request would fail.
	// Its goroutines are reaped at process exit. De-globalizing the pool in
	// evmrpc is the proper fix for repeated Start/Close in one process.

	if net.ownBaseDir && net.baseDir != "" {
		_ = os.RemoveAll(net.baseDir)
	}
}

// stopNode shuts one node's tendermint service and EVM listeners. Each step is
// guarded so a nil (never-started) field on a partial-start path is a no-op.
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
	for _, p := range []string{"tcp://", "http://"} {
		if len(addr) >= len(p) && addr[:len(p)] == p {
			return addr[len(p):]
		}
	}
	return addr
}
