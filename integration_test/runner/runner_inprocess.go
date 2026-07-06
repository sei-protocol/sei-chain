//go:build inprocess

// This file installs the runner's in-process backend. It is gated behind the
// `inprocess` build tag so the heavy inprocess.Network bring-up (and its
// sei-tendermint/sei-cosmos graph) never enters a normal runner build — the
// docker arm in runner.go stays the only backend without the tag.
package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/inprocess"
)

// nodeSource is the backend-agnostic surface the runner needs from a harness
// network: index a validator and count them. Both inprocess.Network (Tier-1,
// in-goroutine) and inprocess.SubprocessNetwork (Tier-2, real `seid` processes)
// satisfy it and return the same inprocess.Node handle, so one execer drives
// either backend — the seid CLIENT commands the suites run don't care whether the
// node they target is a goroutine or a subprocess.
type nodeSource interface {
	Node(i int) inprocess.Node
	Len() int
}

// WithInProcessNetwork selects the Tier-1 in-process backend: commands run on the
// HOST against a real `seid` binary pointed at one of net's in-goroutine nodes,
// with no docker. The Input `node:` field ("sei-node-N", default "sei-node-0")
// selects the node; the command's `seid` invocations are redirected to that node's
// home (and its loopback TM RPC / EVM endpoints) so suites written for the docker
// cluster run unchanged.
func WithInProcessNetwork(net *inprocess.Network) Option {
	return withExecer(newHostExecer(net))
}

// WithSubprocessNetwork selects the Tier-2 subprocess backend: the same host-side
// seid client execer as WithInProcessNetwork, but targeting a cluster of real
// `seid` processes (see inprocess.SubprocessNetwork) instead of in-goroutine
// nodes. Suites run unchanged — the node handle surface is identical — while the
// nodes are now killable/restartable OS processes, which is what the operational
// suites need.
func WithSubprocessNetwork(sn *inprocess.SubprocessNetwork) Option {
	return withExecer(newHostExecer(sn))
}

// hostExecer runs seid client commands on the host against a harness network
// (either backend — see nodeSource). It shims `seid` so opaque sourced helper
// scripts (which call bare `seid` / `$seidbin`) land on the right node: the shim
// prepends `--home "$SEID_HOME"` to every real seid call and `--node "$SEID_NODE"`
// only to the RPC-reading client subcommands (q/query/tx/status — see shimScript),
// and the per-node client.toml the harness wrote under that home supplies chain-id
// and the test keyring.
type hostExecer struct {
	net nodeSource

	once   sync.Once
	binDir string // dir holding the seid shim + real binary, prepended to PATH
	setup  error  // first-build error, returned to every run after
}

func newHostExecer(net nodeSource) *hostExecer {
	return &hostExecer{net: net}
}

// run resolves node → harness node, sets the per-node targeting env (SEID_HOME
// for the shim, SEI_EVM_RPC/WS for curl/EVM commands) plus the accumulated
// capture env, and runs the command on the host. Non-zero command exit is
// reported via stdout (the captured code), matching the docker arm + runner.py
// contract; err is reserved for harness-level failures.
func (e *hostExecer) run(t *testing.T, cmd, node string, envMap map[string]string, opts Options) (string, error) {
	t.Helper()
	if err := e.ensureBin(t); err != nil {
		return "", fmt.Errorf("prepare seid: %w", err)
	}
	h, err := e.nodeFor(node)
	if err != nil {
		return "", err
	}

	c := exec.Command(opts.Shell, "-c", cmd) //nolint:gosec
	// Run from the repo root so the suites' relative `source
	// integration_test/utils/_tx_helpers.sh` resolves (docker runs with the repo
	// mounted at the container CWD; `go test` runs with CWD = the package dir).
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	c.Dir = root
	c.Env = append(os.Environ(), envMapSlice(envMap)...)
	c.Env = append(c.Env,
		"PATH="+e.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SEID_HOME="+h.Home(),
		// SEID_NODE makes TM RPC targeting explicit via the shim's --node flag
		// rather than resting solely on the per-node client.toml. RPCNodeAddr is the
		// tcp:// form --node wants.
		"SEID_NODE="+h.RPCNodeAddr(),
		"SEI_EVM_RPC="+h.EVMRPC(),
		"SEI_EVM_WS="+h.EVMWS(),
		// Some EVM suites read EVM_RPC; keep parity with SEI_EVM_RPC.
		"EVM_RPC="+h.EVMRPC(),
		// The snapshot suite checks this node's snapshot dir. Docker hardcodes its
		// path; this env lets the suite stay backend-portable (the docker arm keeps
		// its literal fallback — see snapshot_operation.yaml).
		"SEI_SNAPSHOT_DIR="+filepath.Join(h.Home(), "snapshots"),
	)

	out, err := c.Output()
	stdout := strings.TrimSpace(string(out))
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Logf("    (exit %d) stderr: %s", exit.ExitCode(), strings.TrimSpace(string(exit.Stderr)))
			return stdout, nil
		}
		return stdout, err
	}
	return stdout, nil
}

// nodeFor maps a "sei-node-N" moniker (the docker container naming the suites
// use) to the harness node at index N. An empty string defaults to node 0, the
// suite default (admin's home). A name without the "sei-node-" prefix is matched
// against node monikers, which resolves the subprocess backend's late-joining
// "sei-rpc-node" (the statesync suite's target).
func (e *hostExecer) nodeFor(node string) (inprocess.Node, error) {
	if node == "" {
		return e.net.Node(0), nil
	}
	if s, ok := strings.CutPrefix(node, "sei-node-"); ok {
		idx, err := strconv.Atoi(s)
		if err != nil {
			return inprocess.Node{}, fmt.Errorf("host arm: node %q has non-numeric index: %w", node, err)
		}
		if idx < 0 || idx >= e.net.Len() {
			return inprocess.Node{}, fmt.Errorf("host arm: node index %d out of range [0,%d)", idx, e.net.Len())
		}
		return e.net.Node(idx), nil
	}
	for i := 0; i < e.net.Len(); i++ {
		if h := e.net.Node(i); h.Name() == node {
			return h, nil
		}
	}
	return inprocess.Node{}, fmt.Errorf("host arm: no node named %q (not sei-node-N and no matching moniker)", node)
}

// prepare is the backendPreparer hook: it runs the one-time build (via ensureBin)
// against the parent test. See ensureBin for why the parent owns the cleanup.
func (e *hostExecer) prepare(t *testing.T) error {
	t.Helper()
	return e.ensureBin(t)
}

// ensureBin builds the seid binary once and writes a `seid` shim alongside it,
// in a dir prepended to PATH. The shim redirects bare `seid` calls (inside
// opaque sourced helpers) to the per-command node home + RPC without rewriting
// the commands — see shimScript for the --home/--node split. The build is on the
// same branch as the harness, so the CLI and the in-process app are the same
// code. The binary is shared across the cases of one RunFile (one execer, one
// sync.Once).
//
// t.Cleanup registers on whichever test first triggers the build; prepare makes
// that the parent test, so the binary outlives every per-case subtest. Cases run
// serially — the unsynchronized binDir read in run is safe only without t.Parallel.
func (e *hostExecer) ensureBin(t *testing.T) error {
	e.once.Do(func() {
		dir, err := os.MkdirTemp("", "sei-inprocess-bin-")
		if err != nil {
			e.setup = err
			return
		}
		e.binDir = dir
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		root, err := repoRoot()
		if err != nil {
			e.setup = err
			return
		}
		realBin := filepath.Join(dir, "seid.real")
		// Build from this branch's source so the CLI matches the in-process app.
		build := exec.Command("go", "build", "-tags", "inprocess", "-o", realBin, "./cmd/seid") //nolint:gosec
		build.Dir = root
		if out, berr := build.CombinedOutput(); berr != nil {
			e.setup = fmt.Errorf("go build seid: %w\n%s", berr, out)
			return
		}

		shim := filepath.Join(dir, "seid")
		script := shimScript(realBin)
		if werr := os.WriteFile(shim, []byte(script), 0o700); werr != nil { //nolint:gosec
			e.setup = werr
			return
		}
	})
	return e.setup
}

// shimScript builds the `seid` shim. Rule: always prepend --home; prepend --node
// only for client subcommands (q/query/tx/status), which register it
// (AddQueryFlagsToCmd / AddTxFlagsToCmd / StatusCommand) — passing --node to
// `keys` or another non-client subcommand fails cobra flag parsing. The allowlist
// is the maintained contract: a new RPC-reading subcommand must be added here.
//
// The subcommand is taken as $1. A leading global flag (`seid --output json query
// …`) makes $1 a flag, not the subcommand, so --node targeting can't be resolved —
// the shim errors (exit 64) rather than silently target the default RPC.
//
// Built as a raw-string template; %q renders the path as a quoted shell token, and
// args are quoted and passed positionally, so a home path with a space is safe.
func shimScript(realBin string) string {
	return fmt.Sprintf(`#!/bin/sh
bin=%q
case "$1" in
  -*)
    echo "inprocess seid shim: leading global flag '$1' blocks --node targeting; call as 'seid <subcommand> ...'" >&2
    exit 64 ;;
  q|query|tx|status)
    exec "$bin" --home "$SEID_HOME" --node "$SEID_NODE" "$@" ;;
  *)
    exec "$bin" --home "$SEID_HOME" "$@" ;;
esac
`, realBin)
}

// repoRoot returns the sei-chain repo root by walking up from this source file's
// package dir (integration_test/runner) to the module root, so `go build
// ./cmd/seid` resolves regardless of the test's working directory. It surfaces a
// Getwd failure rather than silently degrading to "." (a wrong build/run dir),
// which would fail confusingly downstream.
func repoRoot() (string, error) {
	// `go test` runs with CWD = the package dir; runner lives at
	// <root>/integration_test/runner, so climb two levels.
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..")), nil
}
