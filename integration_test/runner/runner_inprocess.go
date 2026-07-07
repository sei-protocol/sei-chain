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

// InProcessSuite runs several YAML files against one shared network with a single
// one-time setup (build → optional keyring overlay → optional fixture script),
// then reuses it for every RunFile. Use it when a group of files shares fixture
// state a per-file RunFile would rebuild — the wasm suites read one gringotts
// deploy + one keyring. A plain RunFile suffices when files are independent (e.g.
// authz, which wants a fresh overlay per file).
type InProcessSuite struct {
	t    *testing.T
	opts Options
}

// NewInProcessSuite binds net, runs the one-time setup once (see runSuiteSetup for
// hook ordering), and returns a suite whose RunFile reuses it. Pass the setup
// options (WithSetupScripts, WithIsolatedKeyring); the network is bound here, so
// WithInProcessNetwork is not needed. Setup and every RunFile run on t, so the
// keyring overlay and seid binary outlive all subtests.
func NewInProcessSuite(t *testing.T, net *inprocess.Network, opts ...Option) *InProcessSuite {
	t.Helper()
	e := newInProcessExecer(net)
	// Bind the execer last so it wins over any stray WithInProcessNetwork.
	o := newOptions(append(append([]Option{}, opts...), withExecer(e)))
	runSuiteSetup(t, o)
	return &InProcessSuite{t: t, opts: o}
}

// RunFile runs one YAML file against the suite's shared setup. Unlike the
// package-level RunFile it does not re-run setup, and it uses the suite's own test
// so the cases stay within the setup's lifetime.
func (s *InProcessSuite) RunFile(path string) {
	s.t.Helper()
	runCases(s.t, path, s.opts)
}

// inProcessExecer runs commands on the host against an inprocess.Network. It
// shims `seid` so opaque sourced helper scripts (which call bare `seid` /
// `$seidbin`) land on the right node: the shim prepends `--home "$SEID_HOME"`
// and `--node "$SEID_NODE"` to every real seid call (explicit home + RPC
// targeting), and the per-node client.toml the harness wrote under that home
// supplies chain-id and the test keyring.
type inProcessExecer struct {
	net *inprocess.Network

	once   sync.Once
	binDir string // dir holding the seid shim + real binary, prepended to PATH
	setup  error  // first-build error, returned to every run after

	// overlayHomes maps a node's real home to a per-RunFile keyring-isolated clone,
	// populated by isolateKeyring when Options.IsolateKeyring is set (nil otherwise ⇒
	// commands use the real home). Written once, before any case runs; read by run —
	// no concurrent access, since cases run sequentially.
	overlayHomes map[string]string
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
	if err := e.ensureBin(t); err != nil {
		return "", fmt.Errorf("prepare seid: %w", err)
	}
	c, err := e.command(t, cmd, node, envMap, opts)
	if err != nil {
		return "", err
	}
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

// command builds the host exec.Cmd for cmd targeted at node: the seid shim on
// PATH, the per-node targeting env (SEID_HOME → the overlay clone when the
// RunFile is keyring-isolated, else the real home; SEID_NODE; the EVM
// endpoints), the accumulated capture env, and CWD at the repo root. Shared by
// run (which swallows non-zero exit per the docker contract) and runSetup (which
// treats it as fatal). Callers ensure the binary is built (ensureBin) first.
func (e *inProcessExecer) command(t *testing.T, cmd, node string, envMap map[string]string, opts Options) (*exec.Cmd, error) {
	t.Helper()
	h, err := e.nodeFor(node)
	if err != nil {
		return nil, err
	}
	c := exec.Command(opts.Shell, "-c", cmd) //nolint:gosec
	// Run from the repo root so the suites' relative `source
	// integration_test/utils/_tx_helpers.sh` resolves (docker runs with the repo
	// mounted at the container CWD; `go test` runs with CWD = the package dir).
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	c.Dir = root
	home := h.Home()
	if ov, ok := e.overlayHomes[home]; ok {
		home = ov
	}
	c.Env = append(os.Environ(), envMapSlice(envMap)...)
	c.Env = append(c.Env,
		"PATH="+e.binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SEID_HOME="+home,
		// SEID_NODE makes TM RPC targeting explicit via the shim's --node flag
		// rather than resting solely on the per-node client.toml. RPCNodeAddr is the
		// tcp:// form --node wants.
		"SEID_NODE="+h.RPCNodeAddr(),
		"SEI_EVM_RPC="+h.EVMRPC(),
		"SEI_EVM_WS="+h.EVMWS(),
		// EVM_RPC / EVM_RPC_URL are the names EVM suites + cast-based fixtures read;
		// alias both to the node's EVM endpoint (dynamic port, so the docker suites'
		// hardcoded :8545 must be repointed to these).
		"EVM_RPC="+h.EVMRPC(),
		"EVM_RPC_URL="+h.EVMRPC(),
	)
	return c, nil
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

// prepare is the backendPreparer hook: it runs the one-time build (via ensureBin)
// against the parent test. See ensureBin for why the parent owns the cleanup.
func (e *inProcessExecer) prepare(t *testing.T) error {
	t.Helper()
	return e.ensureBin(t)
}

// isolateKeyring is the keyringIsolator hook: it clones each node's `test` keyring +
// client.toml into a temp overlay home, so a suite that `keys add`s a name (authz's
// grantee) can't collide with a sibling suite or a prior run on the shared keyring.
// run then points the seid shim's --home at the overlay (see overlayHomes); the
// running node keeps its real home. Cloning admin + node_admin (whose privkeys match
// genesis) keeps signing working; only new adds are sandboxed. Registered on the
// parent test so the overlays outlive the per-case subtests.
func (e *inProcessExecer) isolateKeyring(t *testing.T) error {
	t.Helper()
	e.overlayHomes = make(map[string]string, e.net.Len())
	for i := 0; i < e.net.Len(); i++ {
		h := e.net.Node(i)
		overlay, err := os.MkdirTemp("", "sei-keyring-overlay-")
		if err != nil {
			return err
		}
		t.Cleanup(func() { _ = os.RemoveAll(overlay) })
		// Clone keyring-test/ (the `test`-backend keys) + the whole config/ dir, so
		// `seid --home <overlay>` has everything the client path reads (client.toml's
		// keyring-backend + chain-id, plus config.toml/app.toml) and regenerates
		// nothing — the real home is never touched.
		if err := os.CopyFS(filepath.Join(overlay, "keyring-test"), os.DirFS(filepath.Join(h.Home(), "keyring-test"))); err != nil {
			return fmt.Errorf("clone keyring for %s: %w", h.Name(), err)
		}
		if err := os.CopyFS(filepath.Join(overlay, "config"), os.DirFS(filepath.Join(h.Home(), "config"))); err != nil {
			return fmt.Errorf("clone config for %s: %w", h.Name(), err)
		}
		e.overlayHomes[h.Home()] = overlay
	}
	return nil
}

// runSetup is the setupRunner hook: it runs the suite's fixture scripts in order,
// once, through the same shimmed environment the cases use (bare `seid` lands on
// the target node; the node's EVM endpoint in EVM_RPC_URL; CWD at the repo root),
// before any case, with the caller's fixture-specific opts.SetupEnv layered on top.
// Unlike run, a non-zero script exit is fatal: a failed fixture must fail the
// suite, not silently leave the cases to assert against missing state.
func (e *inProcessExecer) runSetup(t *testing.T, opts Options) error {
	t.Helper()
	if err := e.ensureBin(t); err != nil {
		return fmt.Errorf("prepare seid: %w", err)
	}
	// Fixtures write outputs into the repo tree; register cleanup first so a clean
	// worktree is restored even if a script below fails partway.
	if err := e.cleanFixtureOutputs(t); err != nil {
		return err
	}
	for _, script := range opts.SetupScripts {
		// node "" → node 0 (admin's home), the suites' default signing home.
		c, err := e.command(t, "bash "+script, "", opts.SetupEnv, opts)
		if err != nil {
			return err
		}
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("setup script %s: %w\n%s", script, err, out)
		}
	}
	return nil
}

// cleanFixtureOutputs registers a t.Cleanup that git-cleans the ignored files a
// fixture wrote under integration_test/contracts, so an in-process run leaves no
// worktree diff. `git clean -X` removes only git-ignored files — never a tracked
// input or a developer's untracked source — so it assumes fixtures write only
// ignored outputs there: the wired fixtures (flatkv, timelocked) write *.txt
// (ignored by `integration_test/**/*.txt`) and send other artifacts to /tmp. A
// future fixture emitting a non-ignored file into that dir would be left behind.
// Also self-heals a prior -timeout-killed run's leftovers (t.Cleanup is skipped
// then; see TestMain).
func (e *inProcessExecer) cleanFixtureOutputs(t *testing.T) error {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		return err
	}
	t.Cleanup(func() {
		// -X removes only ignored files (never tracked or a dev's untracked source);
		// -f is required for clean to act, -d recurses into ignored subdirs.
		out, err := exec.Command("git", "-C", root, "clean", "-fdX", "--", "integration_test/contracts").CombinedOutput() //nolint:gosec
		if err != nil {
			t.Errorf("clean fixture outputs: %v\n%s", err, out)
		}
	})
	return nil
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
func (e *inProcessExecer) ensureBin(t *testing.T) error {
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
