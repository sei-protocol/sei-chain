//go:build inprocess

// In-process arm for the EVM RPC .io/.iox conformance + WebSocket suites. Those live
// in sibling packages (rpc_io_test, ws_test) that connect to an EVM RPC purely via env
// and run under `go test`; these drivers seed the shared network (Go, via the seid
// shim + HTTP receipt polls — no docker) and shell the untagged suites against it. The
// suite packages stay byte-unchanged, so the docker path is unaffected.
package runner_test

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/inprocess"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// ioRecipient is the fixed .io/.iox recipient the seeder funds (matches evm_rpc_tests.sh).
const ioRecipient = "0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52"

// seedHTTP bounds each receipt/height poll so a hung request can't outlast the loop
// deadline (which is only checked between requests) and strand t.Cleanup behind a
// go-test timeout panic.
var seedHTTP = &http.Client{Timeout: 5 * time.Second}

// TestInProcessEVMRPCIo runs the execution-apis .io/.iox conformance suite against the
// shared network. The Go seeder (associate + send + deploy minimal/reverter, via the
// seid shim) produces the SEED_BLOCK/REVERTER/DEPLOY env the suite's __SEED__/__REVERTER__
// tags resolve from; SEI_EVM_RPC_URL points it at the node (not SEI_EVM_RPC — the suite
// reads the _URL name). The child `go test` is the same untagged binary docker runs.
func TestInProcessEVMRPCIo(t *testing.T) {
	env := seedEVMIo(t, sharedNet, 0)
	cmd := exec.Command("go", "test", "-count=1", "-v", "./integration_test/evm_module/rpc_io_test/")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rpc_io conformance suite failed: %v\n%s", err, out)
	}
}

// TestInProcessEVMWs runs the WebSocket suite (eth_subscribe newHeads) against the
// shared network's WS endpoint. No seeding needed — it only needs a live net producing
// heads.
func TestInProcessEVMWs(t *testing.T) {
	cmd := exec.Command("go", "test", "-count=1", "-v", "./integration_test/evm_module/ws_test/")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"SEI_EVM_WS_RUN_INTEGRATION=1",
		"SEI_EVM_WS_URL="+sharedNet.Node(0).EVMWS(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ws suite failed: %v\n%s", err, out)
	}
}

// seedEVMIo seeds net's node with the fixtures the .io/.iox suite expects — associates
// admin, sends a wei transfer, and deploys the minimal + reverter contracts — then
// returns the env the suite reads. Seeding runs through the seid shim (InProcessEVMEnv).
func seedEVMIo(t *testing.T, net *inprocess.Network, node int) []string {
	t.Helper()
	base := runner.InProcessEVMEnv(t, net, node)
	evmRPC := net.Node(node).EVMRPC()
	root := repoRoot(t)

	// The shim injects --home, but seid resolves the keyring from the built-in default
	// node home (~/.sei/keyring-test), not --home, so admin (in the harness's
	// <home>/keyring-test) isn't found. --keyring-dir points at that home; --keyring-backend
	// is what forces the rebuild — the tx keyring is constructed once in the root pre-run,
	// and client.ReadPersistentCommandFlags only rebuilds it when --keyring-backend changes,
	// not on a --keyring-dir change alone. Both are needed together.
	keyringDir := net.Node(node).Home()

	// seid runs the shimmed binary; when fatal, a non-zero exit fails the seed loudly
	// (with the combined output) rather than leaving a downstream tag silently skipped.
	seid := func(fatal bool, args ...string) string {
		t.Helper()
		args = append(args, "--keyring-dir", keyringDir, "--keyring-backend", "test")
		c := exec.Command("seid", args...) //nolint:gosec
		c.Dir = root
		c.Env = append(os.Environ(), base...)
		out, err := c.CombinedOutput()
		if err != nil && fatal {
			t.Fatalf("seid %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	// associate-address is a cosmos tx whose pending sequence the EVM mempool's
	// pending-nonce can't see; -b block commits it before the first EVM tx (send) so
	// the two can't sign the same account sequence. Its error is tolerated — an earlier
	// suite on the shared net may already have associated admin.
	seid(false, "tx", "evm", "associate-address", "--from", "admin", "--chain-id", chainID, "-b", "block", "-y")
	// send + the deploys are EVM txs; getNonce reads the mempool-aware pending nonce,
	// so they self-serialize without a per-tx barrier.
	seid(true, "tx", "evm", "send", ioRecipient, "1", "--from", "admin", "--chain-id", chainID, "--evm-rpc", evmRPC, "-b", "sync", "-y")

	const hexDir = "integration_test/evm_module/scripts/contracts"
	minHexPath := hexFile(t, filepath.Join(root, hexDir, "minimal_contract.hex"))
	deployTx := extractTxHash(seid(true, "tx", "evm", "deploy", minHexPath, "--from", "admin", "--chain-id", chainID, "--evm-rpc", evmRPC, "-b", "sync", "-y"))
	seedBlock := pollReceiptField(t, evmRPC, deployTx, "blockNumber")

	revHexPath := hexFile(t, filepath.Join(root, hexDir, "reverter_contract.hex"))
	reverterTx := extractTxHash(seid(true, "tx", "evm", "deploy", revHexPath, "--from", "admin", "--chain-id", chainID, "--evm-rpc", evmRPC, "-b", "sync", "-y"))
	reverterAddr := pollReceiptField(t, evmRPC, reverterTx, "contractAddress")

	// The suite skips (not fails) fixtures whose bindings are empty, so a no-op'd seed
	// would pass green — assert the deploy-derived bindings actually landed.
	if deployTx == "" || seedBlock == "" || reverterAddr == "" {
		t.Fatalf("seed incomplete: deployTx=%q seedBlock=%q reverterAddr=%q", deployTx, seedBlock, reverterAddr)
	}

	// fee-history.io queries a fixed newestBlock (0x1b=27); wait past it with margin so
	// a young chain doesn't error the speconly check (feeHistory needs the block).
	waitForBlockHeight(t, evmRPC, 32)

	return []string{
		"SEI_EVM_IO_RUN_INTEGRATION=1",
		"SEI_EVM_RPC_URL=" + evmRPC,
		"SEI_EVM_IO_SEED_BLOCK=" + seedBlock,
		"SEI_EVM_IO_DEPLOY_TX_HASH=" + deployTx,
		"SEI_EVM_IO_REVERTER_ADDRESS=" + reverterAddr,
	}
}

// repoRoot returns the sei-chain root (the runner package sits at <root>/integration_test/runner).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// hexFile writes src's whitespace-stripped bytes to a temp file and returns its path;
// `seid tx evm deploy` takes a file path and rejects hex containing whitespace.
func hexFile(t *testing.T, src string) string {
	t.Helper()
	raw, err := os.ReadFile(src) //nolint:gosec
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.WriteFile(dst, []byte(strings.Join(strings.Fields(string(raw)), "")), 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
	return dst
}

var txHashRE = regexp.MustCompile(`0x[a-fA-F0-9]{64}`)

// extractTxHash pulls the first 0x-tx-hash out of a `seid tx evm` response.
func extractTxHash(out string) string { return txHashRE.FindString(out) }

// waitForBlockHeight polls eth_blockNumber until the head reaches minHeight, or fails
// after 60s (comfortably above the ~32s a 1s-commit chain takes to reach block 32 from
// genesis — a stalled chain, not a slow one).
func waitForBlockHeight(t *testing.T, evmRPC string, minHeight int64) {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := seedHTTP.Post(evmRPC, "application/json", bytes.NewReader(body))
		if err == nil {
			var parsed struct {
				Result string `json:"result"`
			}
			decErr := json.NewDecoder(resp.Body).Decode(&parsed)
			resp.Body.Close()
			if decErr == nil {
				if h, ok := new(big.Int).SetString(strings.TrimPrefix(parsed.Result, "0x"), 16); ok && h.Int64() >= minHeight {
					return
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("chain did not reach block %d within 60s", minHeight)
}

// pollReceiptField polls eth_getTransactionReceipt for txHash and returns the named
// string field (blockNumber / contractAddress), retrying ~12s for inclusion. Returns ""
// if txHash is empty or the field never appears; seedEVMIo asserts the result is
// non-empty so a missing receipt fails loudly rather than skipping the suite.
func pollReceiptField(t *testing.T, evmRPC, txHash, field string) string {
	t.Helper()
	if txHash == "" {
		return ""
	}
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + txHash + `"]}`)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := seedHTTP.Post(evmRPC, "application/json", bytes.NewReader(body))
		if err == nil {
			var parsed struct {
				Result map[string]json.RawMessage `json:"result"`
			}
			decErr := json.NewDecoder(resp.Body).Decode(&parsed)
			resp.Body.Close()
			if decErr == nil {
				if v, ok := parsed.Result[field]; ok {
					var s string
					if json.Unmarshal(v, &s) == nil && s != "" {
						return s
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}
