//go:build inprocess

// In-process arm for the EVM RPC Parity suite (integration_test/rpc_tests): a
// byte-for-byte diff of Sei's EVM JSON-RPC against an UPSTREAM go-ethereum
// reference node. The Sei side ports by env alone (the suite's endpoints are env-
// defaulted); the one thing the harness does not provide is the reference oracle —
// a stock geth pinned to GETH_VERSION (NOT Sei's vendored fork), run as `geth --dev`.
// This driver provisions that geth, launches it, seeds a pre-funded + pre-associated
// admin (the bootstrap's SEI_ADMIN_MNEMONIC path — its designed non-docker seam), and
// shells the untagged mocha suite (bootstrap then run) against both RPCs.
//
// SEI_ADMIN_MNEMONIC + SEI_COSMOS_RPC + RPC_ETH_GETH are the same env knobs run-ci.sh
// sets. The suite edits are all env-collapsing on SEI_IN_PROCESS — seidNodeExec runs bare
// shimmed `seid` in-process vs `docker exec sei-node-0`; the dual-VM CW20 fixtures register
// their pointer through the shim + dynamic --evm-rpc; eth_coinbase's fee-sweep runs against
// either backend; eth_accounts skips its keyring-dependent cases — so with SEI_IN_PROCESS
// unset the docker arm is byte-identical. The dual-VM CW20/pointer specs (Sei's signature
// divergence from geth) RUN in-process, exercising the pointer-log path a Sei node exists to
// cover.
//
// One in-process gap remains: eth_accounts (5 cases) skips. The EVM server reads its keyring
// from the built-in DefaultNodeHome (~/.sei) — info.go via app.go — not the harness's
// per-node temp home, so it can't see the harness keys without a prod app.go change. The
// only other skips are the suite's own docker-also-skip cases (feeHistory blockCount-0).
//
// Ordering: the bootstrap stores wasm (cw20_base), so this file's
// name must sort AFTER runner_inprocess_test.go, whose TestInProcessSeiDBModule asserts a
// pristine max_code_id==3 baseline a prior wasm store would break — same discipline as
// runner_steaknft_inprocess_test.go. ("rpcparity" > "inprocess", so it does.)
package runner_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/inprocess"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// gethVersion pins the reference go-ethereum. It must match run-ci.sh's GETH_VERSION
// default: the parity error/schema envelopes are geth-version-specific, so a drifted
// reference would diff against the wrong oracle. Confirmed against the installed binary
// (gethBinIsPinned) so a stale cache can't masquerade as the pin.
const gethVersion = "v1.17.0"

// parityAdmin* is a FIXED throwaway test identity (not a secret): the driver funds it
// from the genesis admin and associates it, then hands the suite the mnemonic via
// SEI_ADMIN_MNEMONIC so its fundAdminOnSei sees a spendable EVM balance and early-returns
// (no docker). Pinned so the derived sei/EVM addresses are stable across runs.
const (
	parityAdminKey      = "rpc_parity_admin"
	parityAdminMnemonic = "still dawn loyal assist give draw leaf need scatter farm spider ceiling cattle dial head earth okay search rhythm umbrella cheese drill vintage metal"
	// parityAdminFundUsei gives the admin far more than the suite spends (a 96-account
	// pool at 5 ETH each + deploys/gas) while staying negligible against the genesis
	// admin's 1e21 usei grant.
	parityAdminFundUsei = "1000000000000000usei"
)

// rpcParityDir is the mocha suite's project dir, relative to this package's CWD
// (integration_test/runner). Mirrors dappDir.
const rpcParityDir = "../../integration_test/rpc_tests"

// rpcParityReadySentinel gates the install/compile. It lives inside node_modules (so
// `npm ci`, which rm -rf's node_modules, clears it) and is written ONLY after both npm ci
// and compile succeed — a bare node_modules existence check would accept a half-populated
// tree from an interrupted install. Same discipline as dappReadySentinel.
const rpcParityReadySentinel = "node_modules/.inprocess-ready"

var (
	gethOnce sync.Once
	gethBin  string
	gethErr  error

	rpcParityOnce sync.Once
	rpcParityErr  error
)

// TestInProcessEVMRPCParity runs the JSON-RPC parity suite against the shared network's
// EVM RPC and a freshly-provisioned geth reference, then diffs. It owns the geth process
// lifecycle (t.Cleanup kills the process group + removes the temp datadir) so a leaked
// `geth --dev` can't outlive the run.
func TestInProcessEVMRPCParity(t *testing.T) {
	ensureRPCParityProject(t)
	bin := ensureGeth(t)
	gethURL := startGeth(t, bin)
	seedParityAdmin(t, sharedNet, 0)

	node := sharedNet.Node(0)
	env := append(os.Environ(), runner.InProcessEVMEnv(t, sharedNet, 0)...)
	env = append(env,
		// The .io arm reads SEI_EVM_RPC (set by InProcessEVMEnv); the parity suite also
		// needs the WS endpoint (subscribe specs), the CometBFT RPC (cosmjs association +
		// bank queries + the dual-VM cosmosBankSend), the geth reference, and the pre-funded
		// admin — the same knobs run-ci.sh sets.
		"SEI_EVM_WS="+node.EVMWS(),
		"SEI_COSMOS_RPC="+node.TendermintRPC(),
		"RPC_ETH_GETH="+gethURL,
		"SEI_ADMIN_MNEMONIC="+parityAdminMnemonic,
	)

	// The bootstrap is the only writer of runtime.json; clear it and the prior run's
	// mochawesome json so a stale fixture or report can't bleed into this run (run-ci.sh
	// does the same before the spec run).
	suiteRoot := filepath.Join(repoRoot(t), "integration_test", "rpc_tests")
	_ = os.Remove(filepath.Join(suiteRoot, "runtime", "runtime.json"))
	if matches, _ := filepath.Glob(filepath.Join(suiteRoot, "reports", "new_rpc", "run*.json")); matches != nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}

	// bootstrap (deploys + funds the account pool, writes runtime.json) must complete
	// before run (reads it); a single process, serial — mirrors run-ci.sh.
	start := time.Now()
	for _, script := range []string{"rpc:bootstrap", "rpc:run"} {
		cmd := exec.Command("npm", "run", script) //nolint:gosec
		cmd.Dir = rpcParityDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("npm run %s failed: %v\n%s", script, err, out)
		}
		t.Logf("npm run %s: ok\n%s", script, tail(out, 40))
	}
	t.Logf("parity suite wall-clock (bootstrap+run): %s", time.Since(start).Round(time.Millisecond))
	logParityStats(t, filepath.Join(suiteRoot, "reports", "new_rpc", "run.json"))
}

// ensureRPCParityProject installs + compiles the mocha suite once per test process,
// mirroring run-ci.sh's `npm ci` + `npm run compile`. Sentinel-gated so a warm checkout
// skips the cold install.
func ensureRPCParityProject(t *testing.T) {
	t.Helper()
	rpcParityOnce.Do(func() {
		if _, err := os.Stat(filepath.Join(rpcParityDir, rpcParityReadySentinel)); err == nil {
			return
		}
		if err := installRPCParityDeps(); err != nil {
			rpcParityErr = err
			return
		}
		compile := exec.Command("npm", "run", "compile") //nolint:gosec
		compile.Dir = rpcParityDir
		compile.Env = os.Environ()
		if out, err := compile.CombinedOutput(); err != nil {
			rpcParityErr = fmt.Errorf("rpc_tests npm run compile: %w\n%s", err, out)
			return
		}
		if err := os.WriteFile(filepath.Join(rpcParityDir, rpcParityReadySentinel), nil, 0o600); err != nil {
			rpcParityErr = fmt.Errorf("write rpc_tests ready sentinel: %w", err)
			return
		}
	})
	if rpcParityErr != nil {
		t.Fatalf("prepare rpc_tests project: %v", rpcParityErr)
	}
}

// installRPCParityDeps populates node_modules. `npm ci` is the CI-faithful, lock-pinned
// path (green under the workflow's Node 22). The committed package-lock.json predates
// npm 11, which computes a larger transitive tree (uglify-js/source-map) than the lock
// pins, so `npm ci`'s strict sync check rejects it under a newer local npm — fall back to
// a lock-free `npm install` that still yields a working tree WITHOUT rewriting the tracked
// lock (--no-package-lock), so a stale-lock env runs without a worktree diff. The platform
// esbuild/keccak binaries arrive as prebuilt optional deps, so npm 11's script gating is
// benign here.
func installRPCParityDeps() error {
	ci := exec.Command("npm", "ci") //nolint:gosec
	ci.Dir = rpcParityDir
	ci.Env = os.Environ()
	if out, err := ci.CombinedOutput(); err == nil {
		return nil
	} else if !bytes.Contains(out, []byte("can only install packages when your package.json and package-lock.json")) {
		return fmt.Errorf("rpc_tests npm ci: %w\n%s", err, out)
	}
	install := exec.Command("npm", "install", "--no-package-lock", "--no-audit", "--no-fund") //nolint:gosec
	install.Dir = rpcParityDir
	install.Env = os.Environ()
	if out, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("rpc_tests npm install (lock-free fallback): %w\n%s", err, out)
	}
	return nil
}

// ensureGeth makes the pinned reference geth available once per test process and returns
// its path. It is sentinel-gated on the versioned binary itself: a present binary that
// reports gethVersion is accepted as-is; otherwise `go install ...@gethVersion` fetches it
// into a per-version cache dir. The version check (not mere existence) is the sentinel, so
// a truncated download or a wrong-version leftover forces a reinstall rather than diffing
// against the wrong oracle. Provisioning is the only reason this suite may be CI-only in a
// hermetic env — a failed install fatals with that framing.
func ensureGeth(t *testing.T) string {
	t.Helper()
	gethOnce.Do(func() {
		cacheDir, err := gethCacheDir()
		if err != nil {
			gethErr = err
			return
		}
		bin := filepath.Join(cacheDir, "geth")
		// Reap any geth + datadir a prior -timeout-aborted run orphaned (t.Cleanup was
		// skipped there) before starting a fresh one. Process-once, so it never touches this
		// run's own geth (not started yet).
		sweepStaleGeth(bin)
		if gethBinIsPinned(bin) {
			gethBin = bin
			return
		}
		if _, err := exec.LookPath("go"); err != nil {
			gethErr = fmt.Errorf("geth %s not cached and no go toolchain to install it: %w", gethVersion, err)
			return
		}
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			gethErr = err
			return
		}
		// `go install pkg@version` runs in module mode regardless of CWD; run it from a
		// neutral temp dir so sei-chain's go.mod/vendor can't force vendor mode, and pin the
		// output to the per-version cache via GOBIN.
		cmd := exec.Command("go", "install", "github.com/ethereum/go-ethereum/cmd/geth@"+gethVersion) //nolint:gosec
		cmd.Dir = os.TempDir()
		cmd.Env = append(os.Environ(), "GOBIN="+cacheDir, "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			gethErr = fmt.Errorf("go install geth@%s failed (no network for go install? RPC-Parity stays a CI-only item here): %w\n%s", gethVersion, err, out)
			return
		}
		if !gethBinIsPinned(bin) {
			gethErr = fmt.Errorf("installed geth at %s does not report %s", bin, gethVersion)
			return
		}
		gethBin = bin
	})
	if gethErr != nil {
		t.Fatalf("provision geth: %v", gethErr)
	}
	return gethBin
}

// sweepStaleGeth reaps leftovers from a prior run whose t.Cleanup was skipped (a -timeout
// abort): geth processes launched from the versioned cache bin, and the sei-parity-geth-*
// temp datadirs. Best-effort — every error is ignored. It targets only this cache bin's
// geth (pkill -f on the pinned path) and the suite's own datadir prefix, so it does not
// touch an unrelated geth; it does assume no OTHER live parity run shares this host (the
// harness already runs one in-process network per process).
func sweepStaleGeth(bin string) {
	_ = exec.Command("pkill", "-f", bin).Run() //nolint:gosec
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "sei-parity-geth-*"))
	for _, m := range matches {
		_ = os.RemoveAll(m)
	}
}

// gethCacheDir is a stable per-version cache under the user cache dir, so the pinned geth
// survives across test runs (the sentinel that makes provisioning idempotent).
func gethCacheDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir for geth: %w", err)
	}
	return filepath.Join(root, "sei-inprocess", "geth-"+gethVersion), nil
}

// gethBinIsPinned reports whether bin exists and `geth version` reports gethVersion
// (matched without the leading "v", the form geth prints, e.g. "Version: 1.17.0-stable").
func gethBinIsPinned(bin string) bool {
	out, err := exec.Command(bin, "version").Output() //nolint:gosec
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Version: "+strings.TrimPrefix(gethVersion, "v"))
}

// startGeth launches `geth --dev` on a free loopback port with a temp datadir, waits for
// its RPC, and returns the reference URL. The process is started in its own process group
// (Setpgid) so t.Cleanup can SIGKILL the whole group (geth may spawn helpers). Cleanup is
// the normal-exit path — but it is NOT a guarantee: a `go test -timeout` abort skips
// t.Cleanup entirely, and because Setpgid detaches geth from the test process's own group
// death, that leaves an orphaned geth + its MkdirTemp datadir behind. ensureGeth's
// startup sweep is the backstop that reaps such leftovers on the next run.
func startGeth(t *testing.T, bin string) string {
	t.Helper()
	datadir, err := os.MkdirTemp("", "sei-parity-geth-")
	if err != nil {
		t.Fatalf("geth datadir: %v", err)
	}
	port := freePort(t)
	logPath := filepath.Join(datadir, "geth.log")
	logFile, err := os.Create(logPath) //nolint:gosec
	if err != nil {
		t.Fatalf("geth log: %v", err)
	}

	// Flags mirror run-ci.sh's `npm run rpc:geth`, plus an explicit temp --datadir.
	cmd := exec.Command(bin, //nolint:gosec
		"--dev",
		"--datadir", datadir,
		"--http", "--http.addr", "127.0.0.1", "--http.port", fmt.Sprint(port),
		"--http.api", "eth,net,web3,debug,txpool",
		"--dev.period", "0",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start geth: %v", err)
	}

	t.Cleanup(func() {
		// Signal the whole process group (negative pid); geth may spawn helpers, so
		// killing only the leader could orphan them. SIGKILL, then reap. Skipped on a
		// -timeout abort — see startGeth's doc + the startup sweep.
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
		logFile.Close()
		_ = os.RemoveAll(datadir)
	})

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForEVMReady(t, url, logPath)
	return url
}

// freePort asks the OS for an unused loopback TCP port. There is a benign gap between the
// close here and geth binding it; on a quiet test host the race is not worth an fd-passing
// dance, and a collision surfaces loudly as a geth-never-came-up failure.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve geth port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// seedParityAdmin gives the suite's admin a spendable EVM balance without docker: recover
// the pinned mnemonic into node's keyring, bank-send usei from the genesis admin, then
// associate (so Sei maps the cosmos balance onto the EVM address). Waits until
// eth_getBalance is non-zero — the exact gate the bootstrap's fundAdminOnSei checks, so its
// early-return fires and it never reaches the docker branch. Runs through the seid shim,
// same as seedEVMIo.
func seedParityAdmin(t *testing.T, net *inprocess.Network, node int) {
	t.Helper()
	base := runner.InProcessEVMEnv(t, net, node)
	root := repoRoot(t)
	evmRPC := net.Node(node).EVMRPC()
	// The shim injects --home, but seid resolves the keyring from the built-in default
	// node home, not --home; --keyring-dir points at the node's home and --keyring-backend
	// forces the tx keyring rebuild. Both are needed together (see seedEVMIo).
	keyringDir := net.Node(node).Home()

	seid := func(fatal bool, stdin string, args ...string) string {
		t.Helper()
		args = append(args, "--keyring-dir", keyringDir, "--keyring-backend", "test")
		c := exec.Command("seid", args...) //nolint:gosec
		c.Dir = root
		c.Env = append(os.Environ(), base...)
		if stdin != "" {
			c.Stdin = strings.NewReader(stdin)
		}
		out, err := c.CombinedOutput()
		if err != nil {
			if fatal {
				t.Fatalf("seid %v: %v\n%s", args, err, out)
			}
			// Non-fatal calls (recover on a warm home, associate when already linked) log
			// for diagnosis; waitForEVMBalance is the real gate.
			t.Logf("seid %v (non-fatal): %v\n%s", args, err, out)
		}
		return string(out)
	}

	// Recover the admin key (idempotent per process — this runs once). The mnemonic prompt
	// goes to stderr; `keys add` leaves evm_address empty (it is derived on read), so the
	// addresses come from a follow-up `keys show`.
	// Non-fatal: a re-run against a home that already holds the key aborts the add (the
	// piped mnemonic answers the override prompt with a non-"y"), which keys show recovers.
	seid(false, parityAdminMnemonic+"\n", "keys", "add", parityAdminKey, "--recover", "--output", "json")
	seiAddr, evmAddr := parseKeyAddresses(t, seid(true, "",
		"keys", "show", parityAdminKey, "--output", "json"))

	// Fund from the genesis admin (cosmos bank send); -b block commits it before the
	// association below so the account has gas to associate with.
	seid(true, "",
		"tx", "bank", "send", "admin", seiAddr, parityAdminFundUsei,
		"--from", "admin", "--chain-id", chainID, "--fees", "100000usei", "-b", "block", "-y")

	// Associate the admin's pubkey so its cosmos balance surfaces on the EVM side. The
	// command sends through the EVM RPC, whose --evm-rpc default (0.0.0.0:8545) misses the
	// dynamic in-process port, so point it at the node explicitly. Tolerated non-fatal: a
	// re-run may find it already associated — the balance wait below is the real gate.
	seid(false, "",
		"tx", "evm", "associate-address", "--from", parityAdminKey, "--evm-rpc", evmRPC,
		"--chain-id", chainID, "--fees", "100000usei", "-b", "block", "-y")

	waitForEVMBalance(t, net.Node(node).EVMRPC(), evmAddr)
}

// parseKeyAddresses extracts the sei (bech32) and EVM (0x) addresses from a
// `seid keys add --output json` response, tolerating leading prompt text on stderr by
// slicing the outer JSON object.
func parseKeyAddresses(t *testing.T, out string) (seiAddr, evmAddr string) {
	t.Helper()
	i, j := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if i < 0 || j < i {
		t.Fatalf("seid keys add: no JSON object in output:\n%s", out)
	}
	var parsed struct {
		Address    string `json:"address"`
		EVMAddress string `json:"evm_address"`
	}
	if err := json.Unmarshal([]byte(out[i:j+1]), &parsed); err != nil {
		t.Fatalf("parse seid keys add JSON: %v\n%s", err, out)
	}
	if parsed.Address == "" || parsed.EVMAddress == "" {
		t.Fatalf("seid keys add: missing address/evm_address:\n%s", out)
	}
	return parsed.Address, parsed.EVMAddress
}

// waitForEVMBalance polls eth_getBalance(addr, latest) until it is non-zero, or fails
// after 60s. Non-zero means the fund + associate landed and Sei exposes the balance on
// the EVM side — the precondition the bootstrap asserts.
func waitForEVMBalance(t *testing.T, evmRPC, addr string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		bal, err := evmRPCResult(evmRPC, "eth_getBalance", addr, "latest")
		if err == nil && bal != "" && bal != "0x0" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("parity admin %s never showed a non-zero EVM balance within 60s (fund/associate failed?)", addr)
}

// waitForEVMReady polls eth_chainId until the endpoint answers, or fails after 60s with a
// tail of the geth log so a boot failure is diagnosable.
func waitForEVMReady(t *testing.T, url, logPath string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if id, err := evmRPCResult(url, "eth_chainId"); err == nil && id != "" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	logTail, _ := os.ReadFile(logPath) //nolint:gosec
	t.Fatalf("geth never answered eth_chainId at %s within 60s\n%s", url, tail(logTail, 20))
}

// evmRPCResult makes a single JSON-RPC call and returns the string "result" (empty on any
// non-string / error result). Params are passed positionally.
func evmRPCResult(url, method string, params ...interface{}) (string, error) {
	if params == nil {
		params = []interface{}{}
	}
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return "", err
	}
	resp, err := parityHTTP.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var parsed struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	return parsed.Result, nil
}

// parityHTTP bounds each probe so a hung request can't outlast the poll deadline.
var parityHTTP = &http.Client{Timeout: 5 * time.Second}

// logParityStats reports the mochawesome pass/fail/pending counts for the tracker, best-
// effort (a missing/garbled report just logs a note — the npm exit code already gated pass).
func logParityStats(t *testing.T, runReport string) {
	t.Helper()
	raw, err := os.ReadFile(runReport) //nolint:gosec
	if err != nil {
		t.Logf("parity stats: run report unavailable (%v)", err)
		return
	}
	var report struct {
		Stats struct {
			Tests    int `json:"tests"`
			Passes   int `json:"passes"`
			Failures int `json:"failures"`
			Pending  int `json:"pending"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Logf("parity stats: parse failed (%v)", err)
		return
	}
	t.Logf("parity results: %d tests, %d passing, %d failing, %d pending",
		report.Stats.Tests, report.Stats.Passes, report.Stats.Failures, report.Stats.Pending)
}

// tail returns the last n lines of b, for bounded failure/log excerpts.
func tail(b []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
