// Package runner is a Go-native driver for the YAML integration test suite,
//
// It integrates with standard `go test`, so each test case becomes a subtest
// with proper failure attribution, -run filtering, and -v output.
//
// Requires a running Docker cluster. Start one with `make docker-cluster-start`.
//
// Run a single module:
//
//	go test -tags yaml_integration -v ./integration_test/runner/... -run TestBankModule
//
// Run everything:
//
//	go test -tags yaml_integration -v ./integration_test/runner/...
package runner

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCase mirrors the YAML schema used by the existing test files.
type TestCase struct {
	Name      string     `yaml:"name"`
	Inputs    []Input    `yaml:"inputs"`
	Verifiers []Verifier `yaml:"verifiers"`
}

// Input is one command step within a test case.
type Input struct {
	Cmd  string `yaml:"cmd"`
	Env  string `yaml:"env,omitempty"`  // capture trimmed stdout into this name
	Node string `yaml:"node,omitempty"` // docker container; defaults to Options.DefaultContainer
}

// Verifier checks the accumulated env map after all inputs have run.
type Verifier struct {
	Type   string `yaml:"type"` // "eval" or "regex"
	Expr   string `yaml:"expr"`
	Result string `yaml:"result,omitempty"` // env var to match (regex only)
}

// execer runs one command step against a target node and returns its trimmed
// stdout. It is the seam between the two backends: the docker arm runs the
// command via `docker exec`, the in-process arm runs it on the host against an
// inprocess.Network. node is the Input's resolved target (a docker container
// name / a "sei-node-N" moniker); env is the accumulated capture map. A non-zero
// command exit is reported via the returned string (the captured code), not err
// — err is reserved for harness-level failures (mirrors the runner.py contract).
type execer interface {
	run(t *testing.T, cmd, node string, env map[string]string, opts Options) (string, error)
}

// backendPreparer is an optional execer capability: a backend whose one-time
// setup must be scoped to the parent test rather than a per-case subtest
// implements it, and RunFile invokes it before running any case. The in-process
// arm uses it to build its shared seid binary; the docker arm needs no setup and
// does not implement it.
type backendPreparer interface {
	prepare(t *testing.T) error
}

// keyringIsolator is an optional execer capability: an arm that can give a RunFile a
// private keyring implements it, and RunFile invokes it on the parent test (so the
// overlay outlives the per-case subtests) when Options.IsolateKeyring is set. The
// docker arm (per-container keyrings) does not implement it.
type keyringIsolator interface {
	isolateKeyring(t *testing.T) error
}

// setupRunner is an optional execer capability: a backend that can run a suite's
// fixture bring-up script (deploy contracts, seed keys the cases assume) once
// implements it, and RunFile invokes it after keyring isolation and before any
// case when Options.SetupScript is set. The in-process arm runs the script
// through its seid shim; the docker arm (fixtures brought up by the CI harness)
// does not implement it.
type setupRunner interface {
	runSetup(t *testing.T, scriptPath string, opts Options) error
}

// Options controls how RunFile executes commands.
type Options struct {
	// DefaultContainer is the docker container used when an Input has no Node set.
	DefaultContainer string
	// ExtraPath is appended to PATH inside docker containers.
	ExtraPath string
	// Shell is the shell used to execute commands (e.g. "sh", "bash").
	// Resolved via PATH at runtime. Defaults to "sh".
	Shell string
	// exec is the backend. nil selects the docker arm (the default), so existing
	// docker runs are unaffected. The in-process arm is installed via
	// WithInProcessNetwork (build-tagged `inprocess`); it never enters a normal
	// runner build.
	exec execer

	// IsolateKeyring, when true, gives this RunFile a private clone of the target
	// node's `test` keyring so a suite that `keys add`s a name (e.g. authz's grantee)
	// doesn't collide on that name with a sibling suite on the shared network. It
	// isolates the keyring namespace, not on-chain state — a suite adding a fixed key
	// via `--recover` would still land the same address, so on-chain effects still
	// share the chain. Backend-specific: the in-process arm honors it; the docker arm
	// (per-container keyrings) ignores it.
	IsolateKeyring bool

	// SetupScript, when set, is a repo-root-relative fixture bring-up script run
	// once before the cases (deploy the contracts + seed the keys the suites
	// assume). Backend-specific: the in-process arm runs it through its shim
	// (executed at the repo root, so the path is repo-root-relative); the docker
	// arm ignores it (its fixtures are brought up by the CI harness).
	SetupScript string

	// SetupEnv is fixture-specific environment for the SetupScript run (e.g. a
	// keyring backend, a signer name, an RPC target). The in-process arm layers it
	// under its own node-targeting env (SEID_HOME/SEID_NODE/EVM endpoints).
	SetupEnv map[string]string
}

// Option is a functional option for Options.
type Option func(*Options)

// WithContainer sets the default docker container for inputs that don't specify one.
func WithContainer(container string) Option {
	return func(o *Options) { o.DefaultContainer = container }
}

// WithExtraPath overrides the PATH suffix injected into docker containers.
func WithExtraPath(path string) Option {
	return func(o *Options) { o.ExtraPath = path }
}

// WithShell overrides the shell used to execute commands. Resolved via PATH at runtime.
func WithShell(shell string) Option {
	return func(o *Options) { o.Shell = shell }
}

// withExecer installs a backend execer. Unexported: callers select the
// in-process arm via WithInProcessNetwork (build-tagged), and the docker arm is
// the zero-value default.
func withExecer(e execer) Option {
	return func(o *Options) { o.exec = e }
}

// WithIsolatedKeyring gives this RunFile a private keyring clone (see
// Options.IsolateKeyring).
func WithIsolatedKeyring() Option {
	return func(o *Options) { o.IsolateKeyring = true }
}

// WithSetupScript runs a fixture bring-up script once before the cases (see
// Options.SetupScript). The path is repo-root-relative.
func WithSetupScript(path string) Option {
	return func(o *Options) { o.SetupScript = path }
}

// WithSetupEnv sets fixture-specific environment for the SetupScript run (see
// Options.SetupEnv).
func WithSetupEnv(env map[string]string) Option {
	return func(o *Options) { o.SetupEnv = env }
}

func newOptions(opts []Option) Options {
	var o Options
	//applying default options
	for _, f := range []Option{WithContainer("sei-node-0"), WithExtraPath("/root/go/bin:/root/.foundry/bin"), WithShell("bash")} {
		f(&o)
	}
	for _, opt := range opts {
		opt(&o)
	}
	if o.exec == nil {
		o.exec = dockerExecer{}
	}
	return o
}

// RunFile reads path as a YAML list of TestCases and runs each as a subtest of t.
func RunFile(t *testing.T, path string, opts ...Option) {
	t.Helper()
	o := newOptions(opts)
	runSuiteSetup(t, o)
	runCases(t, path, o)
}

// runSuiteSetup runs the one-time, parent-scoped backend hooks in order — the
// backend build, keyring isolation, then the fixture script — so the fixture's
// `keys add` and contract deploys land in the isolated keyring. It is scoped to
// the parent test so it runs before, and outlives, every per-case subtest (see
// ensureBin). RunFile runs it per file; a caller that shares one setup across
// several files (the in-process suite) runs it once. The docker arm implements
// none of these hooks.
func runSuiteSetup(t *testing.T, o Options) {
	t.Helper()
	if p, ok := o.exec.(backendPreparer); ok {
		require.NoError(t, p.prepare(t), "prepare backend")
	}
	if o.IsolateKeyring {
		if iso, ok := o.exec.(keyringIsolator); ok {
			require.NoError(t, iso.isolateKeyring(t), "isolate keyring")
		}
	}
	if o.SetupScript != "" {
		if sr, ok := o.exec.(setupRunner); ok {
			require.NoError(t, sr.runSetup(t, o.SetupScript, o), "run setup script")
		}
	}
}

// runCases parses path as a YAML list of TestCases and runs each as a subtest of
// t; it assumes the one-time setup (runSuiteSetup) already ran.
func runCases(t *testing.T, path string, o Options) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec
	require.NoError(t, err, "read %s: %v", path, err)
	var cases []TestCase
	require.NoError(t, yaml.Unmarshal(data, &cases), "unmarshal %s: %v", path, err)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			runCase(t, tc, o)
		})
	}
}

func runCase(t *testing.T, tc TestCase, opts Options) {
	t.Helper()
	envMap := make(map[string]string)

	for i, inp := range tc.Inputs {
		node := inp.Node
		if node == "" {
			node = opts.DefaultContainer
		}
		out, err := opts.exec.run(t, inp.Cmd, node, envMap, opts)
		t.Logf("[%d] $ %s\n    => %s", i, inp.Cmd, out)
		require.NoError(t, err, "input[%d] failed: %v", i, err)
		if inp.Env != "" {
			envMap[inp.Env] = out
		}
	}

	for _, v := range tc.Verifiers {
		if err := verify(v, envMap); err != nil {
			t.Errorf("verifier %s %q: %v", v.Type, v.Expr, err)
		}
	}
}

// dockerExecer is the default backend: it runs each command via `docker exec`
// in the target container (the existing behavior). It is selected whenever no
// other execer is installed, so docker runs are unaffected by the seam.
type dockerExecer struct{}

// run runs cmd in the given docker container (or locally if container is empty),
// injecting the accumulated envMap. Non-zero exit is logged but not fatal — this
// matches runner.py behaviour where commands that echo error codes exit 0 from
// bash but the captured output is the code.
func (dockerExecer) run(t *testing.T, cmd, container string, envMap map[string]string, opts Options) (string, error) {
	t.Helper()
	var c *exec.Cmd

	if container != "" {
		// capacity: 1 ("exec") + 2*len(envMap) ("-e" + "k=v" per entry) + 4 (container, "/bin/bash", "-c", cmd)
		args := make([]string, 1, 1+2*len(envMap)+4)
		args[0] = "exec"
		for k, v := range envMap {
			args = append(args, "-e", k+"="+v)
		}
		args = append(args, container, opts.Shell, "-c",
			"export PATH=$PATH:"+opts.ExtraPath+" && "+cmd)
		c = exec.Command("docker", args...) //nolint:gosec
	} else {
		c = exec.Command(opts.Shell, "-c", cmd) //nolint:gosec
		c.Env = append(os.Environ(), envMapSlice(envMap)...)
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

// envMapSlice converts envMap to a slice of "K=V" strings suitable for exec.Cmd.Env.
func envMapSlice(envMap map[string]string) []string {
	s := make([]string, 0, len(envMap))
	for k, v := range envMap {
		s = append(s, k+"="+v)
	}
	return s
}

// verify evaluates a single Verifier against the accumulated env map.
func verify(v Verifier, envMap map[string]string) error {
	switch v.Type {
	case "eval":
		ok, err := evalExpr(v.Expr, envMap)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("false: %s%s", v.Expr, fmtRelevant(v.Expr, envMap))
		}
		return nil
	case "regex":
		val := envMap[v.Result]
		ok, err := regexp.MatchString(v.Expr, val)
		if err != nil {
			return fmt.Errorf("bad pattern %q: %w", v.Expr, err)
		}
		if !ok {
			return fmt.Errorf("pattern %q did not match %q", v.Expr, val)
		}
		return nil
	default:
		return fmt.Errorf("unknown verifier type %q", v.Type)
	}
}

// evalExpr evaluates a Python-runner-compatible eval expression.
// Handles: VAR op literal, VAR op VAR, expr and expr, expr or expr.
// Operators: ==, !=, >, <, >=, <=.
func evalExpr(expr string, envMap map[string]string) (bool, error) {
	if parts := strings.Split(expr, " and "); len(parts) > 1 {
		for _, p := range parts {
			ok, err := evalExpr(strings.TrimSpace(p), envMap)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
	if parts := strings.Split(expr, " or "); len(parts) > 1 {
		for _, p := range parts {
			ok, err := evalExpr(strings.TrimSpace(p), envMap)
			if err == nil && ok {
				return true, nil
			}
		}
		return false, nil
	}
	return cmpExpr(strings.TrimSpace(expr), envMap)
}

// cmpExpr evaluates a single "LHS op RHS" comparison.
// It tokenizes by whitespace first and substitutes env var names before
// locating the operator, so arithmetic sub-expressions like EXPECTED_COUNTS + 1
// are evaluated rather than compared as literal strings.
func cmpExpr(expr string, envMap map[string]string) (bool, error) {
	tokens := strings.Fields(expr)
	for i, t := range tokens {
		if len(t) >= 2 && t[0] == '"' && t[len(t)-1] == '"' {
			tokens[i] = t[1 : len(t)-1]
		} else if v, ok := envMap[t]; ok {
			tokens[i] = v
		}
	}
	for i, t := range tokens {
		for _, op := range []string{"!=", ">=", "<=", "==", ">", "<"} {
			if t == op {
				lhs, err := evalArith(tokens[:i])
				if err != nil {
					return false, err
				}
				rhs, err := evalArith(tokens[i+1:])
				if err != nil {
					return false, err
				}
				return cmpValues(lhs, rhs, op)
			}
		}
	}
	return false, fmt.Errorf("no operator in %q", expr)
}

// evalArith evaluates a sequence of tokens as big.Int arithmetic (e.g. ["4", "+", "1"] → "5").
// Falls back to the raw joined string when the first token is not a valid integer,
// allowing float and string comparisons to fall through to cmpValues.
func evalArith(tokens []string) (string, error) {
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty side of comparison")
	}
	if len(tokens) == 1 {
		return tokens[0], nil
	}
	result := new(big.Int)
	if _, ok := result.SetString(tokens[0], 10); !ok {
		return strings.Join(tokens, " "), nil
	}
	for i := 1; i+1 < len(tokens); i += 2 {
		operand := new(big.Int)
		if _, ok := operand.SetString(tokens[i+1], 10); !ok {
			return strings.Join(tokens, " "), nil
		}
		switch tokens[i] {
		case "+":
			result.Add(result, operand)
		case "-":
			result.Sub(result, operand)
		case "*":
			result.Mul(result, operand)
		case "/":
			if operand.Sign() == 0 {
				return "", fmt.Errorf("division by zero in %v", tokens)
			}
			result.Div(result, operand)
		default:
			return strings.Join(tokens, " "), nil
		}
	}
	return result.String(), nil
}

// cmpValues compares a and b with op. Tries big.Int (for large integers without
// float64 precision loss), then float64, then string.
func cmpValues(a, b, op string) (bool, error) {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)

	ai, bi := new(big.Int), new(big.Int)
	if _, ok1 := ai.SetString(a, 10); ok1 {
		if _, ok2 := bi.SetString(b, 10); ok2 {
			return applyOp(ai.Cmp(bi), op)
		}
	}

	fa, errA := strconv.ParseFloat(a, 64)
	fb, errB := strconv.ParseFloat(b, 64)
	if errA == nil && errB == nil {
		c := 0
		if fa < fb {
			c = -1
		} else if fa > fb {
			c = 1
		}
		return applyOp(c, op)
	}

	return applyOp(strings.Compare(a, b), op)
}

func applyOp(cmp int, op string) (bool, error) {
	switch op {
	case "==":
		return cmp == 0, nil
	case "!=":
		return cmp != 0, nil
	case ">":
		return cmp > 0, nil
	case "<":
		return cmp < 0, nil
	case ">=":
		return cmp >= 0, nil
	case "<=":
		return cmp <= 0, nil
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

// fmtRelevant returns a diagnostic string listing env vars referenced in expr.
func fmtRelevant(expr string, envMap map[string]string) string {
	var refs []string
	for k, v := range envMap {
		if strings.Contains(expr, k) {
			refs = append(refs, k+"="+v)
		}
	}
	if len(refs) == 0 {
		return ""
	}
	return " [" + strings.Join(refs, ", ") + "]"
}
