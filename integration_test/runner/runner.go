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
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

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

// Options controls how RunFile executes commands.
type Options struct {
	// DefaultContainer is the docker container used when an Input has no Node set.
	// If empty, commands run locally (useful for tests that don't need docker).
	DefaultContainer string
	// ExtraPath is appended to PATH inside docker containers.
	// Defaults to /root/go/bin:/root/.foundry/bin (matching runner.py).
	ExtraPath string
}

// RunFile reads path as a YAML list of TestCases and runs each as a subtest of t.
func RunFile(t *testing.T, path string, opts Options) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cases []TestCase
	if err = yaml.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			runCase(t, tc, opts)
		})
	}
}

func runCase(t *testing.T, tc TestCase, opts Options) {
	t.Helper()
	envMap := make(map[string]string)

	for i, inp := range tc.Inputs {
		container := inp.Node
		if container == "" {
			container = opts.DefaultContainer
		}
		out, err := execCmd(t, inp.Cmd, container, envMap, opts)
		t.Logf("[%d] $ %s\n    => %s", i, inp.Cmd, out)
		if err != nil {
			t.Fatalf("input[%d] failed: %v", i, err)
		}
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

// execCmd runs cmd in the given docker container (or locally if container is empty),
// injecting the accumulated envMap. Non-zero exit is logged but not fatal — this
// matches runner.py behaviour where commands that echo error codes exit 0 from
// bash but the captured output is the code.
func execCmd(t *testing.T, cmd, container string, envMap map[string]string, opts Options) (string, error) {
	t.Helper()
	var c *exec.Cmd

	if container != "" {
		extraPath := opts.ExtraPath
		if extraPath == "" {
			extraPath = "/root/go/bin:/root/.foundry/bin"
		}
		// capacity: 1 ("exec") + 2*len(envMap) ("-e" + "k=v" per entry) + 4 (container, "/bin/bash", "-c", cmd)
		args := make([]string, 1, 1+2*len(envMap)+4)
		args[0] = "exec"
		for k, v := range envMap {
			args = append(args, "-e", k+"="+v)
		}
		args = append(args, container, "/bin/bash", "-c",
			"export PATH=$PATH:"+extraPath+" && "+cmd)
		c = exec.Command("docker", args...) //nolint:gosec
	} else {
		c = exec.Command("/bin/bash", "-c", cmd)
		c.Env = append(os.Environ(), envMapSlice(envMap)...)
	}

	out, err := c.Output()
	stdout := strings.TrimSpace(string(out))
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			t.Logf("    (exit %d) stderr: %s", exit.ExitCode(), strings.TrimSpace(string(exit.Stderr)))
			return stdout, nil
		}
		return stdout, fmt.Errorf("exec: %w", err)
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
