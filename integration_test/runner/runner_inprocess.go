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

// WithInProcessNetwork selects the in-process backend: commands run on the HOST
// against a real `seid` binary pointed at one of net's in-process nodes, with no
// docker. The Input `node:` field ("sei-node-N", default "sei-node-0") selects
// the node; the command's `seid` invocations are redirected to that node's home
// (and its loopback TM RPC / EVM endpoints) so suites written for the docker
// cluster run unchanged.
//
// The build tag means this option only exists in an `inprocess` build; docker
// runs (built without the tag) cannot reference it, and so cannot regress.
func WithInProcessNetwork(net *inprocess.Network) Option {
	return withExecer(newInProcessExecer(net))
}

// inProcessExecer runs commands on the host against an inprocess.Network. It
// shims `seid` so opaque sourced helper scripts (which call bare `seid` /
// `$seidbin`) land on the right node: the shim prepends `--home "$SEID_HOME"`
// to every real seid call, and the per-node client.toml the harness wrote under
// that home supplies chain-id, the test keyring, and the node's RPC address.
type inProcessExecer struct {
	net *inprocess.Network

	once   sync.Once
	binDir string // dir holding the seid shim + real binary, prepended to PATH
	setup  error  // first-build error, returned to every run after
}

func newInProcessExecer(net *inprocess.Network) *inProcessExecer {
	return &inProcessExecer{net: net}
}

// run resolves node → harness node, sets the per-node targeting env (SEID_HOME
// for the shim, SEI_EVM_RPC/WS for curl/EVM commands) plus the accumulated
// capture env, and runs the command on the host. Non-zero command exit is
// reported via stdout (the captured code), matching the docker arm + runner.py
// contract; err is reserved for harness-level failures.
func (e *inProcessExecer) run(t *testing.T, cmd, node string, envMap map[string]string, opts Options) (string, error) {
	t.Helper()
	if err := e.ensureBin(); err != nil {
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
	c.Dir = repoRoot()
	c.Env = append(os.Environ(), envMapSlice(envMap)...)
	c.Env = append(c.Env,
		"PATH="+e.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SEID_HOME="+h.Home(),
		"SEI_EVM_RPC="+h.EVMRPC(),
		"SEI_EVM_WS="+h.EVMWS(),
		// Some EVM suites read EVM_RPC; keep parity with SEI_EVM_RPC.
		"EVM_RPC="+h.EVMRPC(),
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
// suite default (admin's home).
func (e *inProcessExecer) nodeFor(node string) (inprocess.Node, error) {
	idx := 0
	if node != "" {
		const prefix = "sei-node-"
		s, ok := strings.CutPrefix(node, prefix)
		if !ok {
			return inprocess.Node{}, fmt.Errorf("in-process arm: node %q is not %sN", node, prefix)
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return inprocess.Node{}, fmt.Errorf("in-process arm: node %q has non-numeric index: %w", node, err)
		}
		idx = n
	}
	if idx < 0 || idx >= e.net.Len() {
		return inprocess.Node{}, fmt.Errorf("in-process arm: node index %d out of range [0,%d)", idx, e.net.Len())
	}
	return e.net.Node(idx), nil
}

// ensureBin builds the seid binary once and writes a `seid` shim alongside it,
// in a dir prepended to PATH. The shim execs the real binary with `--home
// "$SEID_HOME"` prepended: --home is a global persistent flag every seid
// subcommand accepts, so a single shim redirects bare `seid` calls (inside
// opaque sourced helpers) to the per-command node home without rewriting the
// commands. The build is on the same branch as the harness, so the CLI and the
// in-process app are the same code.
func (e *inProcessExecer) ensureBin() error {
	e.once.Do(func() {
		dir, err := os.MkdirTemp("", "sei-inprocess-bin-")
		if err != nil {
			e.setup = err
			return
		}
		e.binDir = dir

		realBin := filepath.Join(dir, "seid.real")
		// Build from this branch's source so the CLI matches the in-process app.
		build := exec.Command("go", "build", "-tags", "inprocess", "-o", realBin, "./cmd/seid")
		build.Dir = repoRoot()
		if out, berr := build.CombinedOutput(); berr != nil {
			e.setup = fmt.Errorf("go build seid: %w\n%s", berr, out)
			return
		}

		shim := filepath.Join(dir, "seid")
		// --home is global; prepending it is valid for every subcommand. exec
		// replaces the shim process so signals/exit codes pass through cleanly.
		script := "#!/bin/sh\nexec \"" + realBin + "\" --home \"$SEID_HOME\" \"$@\"\n"
		if werr := os.WriteFile(shim, []byte(script), 0o700); werr != nil { //nolint:gosec
			e.setup = werr
			return
		}
	})
	return e.setup
}

// repoRoot returns the sei-chain repo root by walking up from this source file's
// package dir (integration_test/runner) to the module root, so `go build
// ./cmd/seid` resolves regardless of the test's working directory.
func repoRoot() string {
	// runner package lives at <root>/integration_test/runner; climb two levels.
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	// `go test` runs with CWD = the package dir.
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
