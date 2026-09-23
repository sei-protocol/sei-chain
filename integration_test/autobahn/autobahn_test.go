//go:build autobahn_integration

// Package autobahn contains integration tests for Autobahn EVM-only consensus.
//
// Requires a running Autobahn Docker cluster. Run via:
//
//	make autobahn-integration-test
//
// Or directly (cluster must already be up):
//
//	go test -tags autobahn_integration -v ./integration_test/autobahn/...
package autobahn

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

const (
	// Cluster lifecycle (TestMain).
	clusterBootTimeout  = 5 * time.Minute
	clusterBootPoll     = 5 * time.Second
	autobahnSettleDelay = 30 * time.Second

	// Fullnode sidecar lifecycle (TestMain).
	fullnodeContainer = "sei-rpc-node"
	// fullnodeStartTimeout bounds `make` getting the container running (image
	// build/pull); fullnodeBootTimeout bounds the node's own boot after that.
	fullnodeStartTimeout = 10 * time.Minute
	fullnodeBootTimeout  = 5 * time.Minute
	fullnodeBootPoll     = 5 * time.Second
	// evmRPCURLOnContainerLocalhost is the EVM RPC address inside the
	// rpc-node container — used with `docker exec ... curl` for readiness
	// checks (the rpc-node's 8545 isn't host-published).
	evmRPCURLOnContainerLocalhost = "http://localhost:8545"
	// fullnodeProbeAddress is read for readiness. Any address answers: an
	// address absent from FlatKV reads as the EVM-only default balance.
	fullnodeProbeAddress   = "0x0000000000000000000000000000000000000001"
	fullnodeReceiptTimeout = 2 * time.Minute
	fullnodeReceiptPoll    = 1 * time.Second
	// progressTxAccountBase is past the 4_000 deterministic senders EVMOnlyLoad
	// uses, so a later progress tx is a nonce-0 transfer from a funded account.
	progressTxAccountBase = 1_000_000
	// prebuiltImagesEnv selects run-rpc-node-skipbuild-ci for the sidecar, which
	// runs the already present sei-chain/rpcnode image instead of rebuilding it.
	prebuiltImagesEnv = "AUTOBAHN_PREBUILT_IMAGES"

	// Fault-tolerance subtests. Execution height is read from each
	// validator's own metrics endpoint, so a killed validator drops out of
	// the sample rather than stalling the read.
	heightReadTimeout = 10 * time.Second
	heightPoll        = 1 * time.Second
	livenessTimeout   = 2 * time.Minute
	recoveryTimeout   = 3 * time.Minute
	// haltStableWindow is how long height must stand still to count as a
	// halt; the timeout leaves room for in-flight blocks to drain through
	// runExecute on the validators that are still up.
	haltStableWindow  = 20 * time.Second
	haltStableTimeout = 2 * time.Minute

	evmOnlyLoadTxs     = 4_000
	evmOnlyLoadTimeout = 3 * time.Minute
	evmOnlyMetricsURL  = "http://127.0.0.1:26660/metrics"
	evmOnlyNextBlock   = "tendermint_internal_autobahn_data_next_block"
	evmOnlyTxLatency   = "tendermint_internal_autobahn_data_latency_count"
)

// clusterSize is set once at TestAutobahn start from the number of running
// sei-node-* containers. Subtests read it (and maxFaults) from here.
var (
	clusterSize   int
	maxFaults     int
	progressTxSeq atomic.Uint64
)

// listRunningNodes returns the container names of currently-running
// sei-node-* containers.
func listRunningNodes(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("docker", "ps",
		"--filter", "name=sei-node-",
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("docker ps: %v", err)
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

// assertAutobahnEnabled checks that "GigaRouter initialized" appears in every
// currently-running sei-node-* container's logs. Guards against accidental
// disablement. Scoped to live containers so killed nodes (from earlier tests)
// don't false-positive on stale host-side log files.
func assertAutobahnEnabled(t *testing.T) {
	t.Helper()
	names := listRunningNodes(t)
	if len(names) == 0 {
		t.Fatalf("no running sei-node-* containers")
	}
	for _, name := range names {
		// seid writes logs to a file inside the container (not stdout), so we
		// grep via docker exec rather than `docker logs`. Each container only
		// has its own seid-<id>.log under the repo-relative build/generated/logs.
		cmd := exec.Command("docker", "exec", name, "sh", "-c",
			"grep -q 'GigaRouter initialized' build/generated/logs/seid-*.log")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("autobahn not enabled on %s (no 'GigaRouter initialized' in container log): %v\n%s",
				name, err, out)
		}
	}
}

func assertEVMOnlyEnabled(t *testing.T) {
	t.Helper()
	for _, name := range listRunningNodes(t) {
		cmd := exec.Command("docker", "exec", name, "sh", "-c",
			"grep -q 'Autobahn EVM-only execution enabled with disk-backed Giga storage' build/generated/logs/seid-*.log")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("EVM-only execution not enabled on %s: %v\n%s", name, err, out)
		}
	}
}

func TestMain(m *testing.M) {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find repo root: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "chdir to %s: %v\n", root, err)
		os.Exit(1)
	}
	if err := setupCluster(); err != nil {
		fmt.Fprintf(os.Stderr, "cluster setup failed: %v\n", err)
		teardownCluster()
		os.Exit(1)
	}
	if err := setupFullnodeNode(); err != nil {
		fmt.Fprintf(os.Stderr, "fullnode sidecar setup failed: %v\n", err)
		teardownCluster()
		os.Exit(1)
	}
	code := m.Run()
	teardownCluster()
	os.Exit(code)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// runMake runs `make <target>` from the current directory, streaming output.
func runMake(env []string, target string) error {
	cmd := exec.Command("make", target)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// setupCluster starts the autobahn docker cluster and waits until all nodes
// have signalled readiness via build/generated/launch.complete.
func setupCluster() error {
	fmt.Println("=== Starting Autobahn Integration Tests ===")
	// Best-effort cleanup of any prior cluster, then wipe generated state.
	_ = runMake(nil, "docker-cluster-stop")
	if err := os.RemoveAll("build/generated"); err != nil {
		return fmt.Errorf("rm -rf build/generated: %w", err)
	}
	if err := runMake([]string{"AUTOBAHN=true", "DOCKER_DETACH=true"}, "docker-cluster-start"); err != nil {
		return fmt.Errorf("docker-cluster-start: %w", err)
	}
	expected, err := countSeiContainers()
	if err != nil {
		return fmt.Errorf("count cluster containers: %w", err)
	}
	if expected == 0 {
		return fmt.Errorf("no sei-node-* containers found after docker-cluster-start")
	}
	fmt.Printf("Waiting for %d nodes to be ready...\n", expected)
	deadline := time.Now().Add(clusterBootTimeout)
	for time.Now().Before(deadline) {
		if n := countLaunchComplete("build/generated/launch.complete"); n >= expected {
			fmt.Printf("All %d nodes are ready\n", expected)
			fmt.Printf("Waiting %s for autobahn connections to establish...\n", autobahnSettleDelay)
			time.Sleep(autobahnSettleDelay)
			return nil
		}
		time.Sleep(clusterBootPoll)
	}
	return fmt.Errorf("cluster failed to start within %s", clusterBootTimeout)
}

// countSeiContainers returns the number of sei-node-* containers that exist
// (running or not yet started).
func countSeiContainers() (int, error) {
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name=sei-node-",
		"--format", "{{.Names}}").Output()
	if err != nil {
		return 0, err
	}
	return len(strings.Fields(strings.TrimSpace(string(out)))), nil
}

func prebuiltImages() bool {
	return os.Getenv(prebuiltImagesEnv) == "true"
}

// setupFullnodeNode boots an autobahn fullnode sidecar alongside the validator
// cluster. Backgrounded via cmd.Start() because `make run-rpc-node-skipbuild`
// uses `docker run --rm` (foreground until the container exits); the actual
// container detaches from this process once it starts.
//
// Uses run-rpc-node-skipbuild so the rpc-node reuses the seid binary the
// validator containers already compiled — skips a second multi-minute
// `go install` cycle. The autobahn role itself comes from mode = "full"
// in docker/rpcnode/config/config.toml — setup.go picks the fullnode
// constructor when there's no local validator key.
func setupFullnodeNode() error {
	fmt.Println("=== Starting fullnode sidecar ===")
	_ = runMake(nil, "kill-rpc-node") // best-effort cleanup

	// Discover the cluster size from docker so the rpc-node's autobahn config
	// covers exactly the validators that came up — non-four-node test runs
	// would otherwise produce a mismatched committee.
	clusterSize, err := countSeiContainers()
	if err != nil {
		return fmt.Errorf("count cluster containers: %w", err)
	}
	if clusterSize == 0 {
		return fmt.Errorf("no sei-node-* containers found; setupCluster must run first")
	}
	target := "run-rpc-node-skipbuild"
	if prebuiltImages() {
		target += "-ci"
	}
	cmd := exec.Command("make", target)
	cmd.Env = append(os.Environ(), "AUTOBAHN=true", fmt.Sprintf("CLUSTER_SIZE=%d", clusterSize))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start make %s: %w", target, err)
	}
	// Reap the process when it eventually exits (e.g. on container kill);
	// not blocking on Wait here since the container runs for the duration
	// of the test suite.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Phase 1: wait for the container to exist and run. Anything `make` does
	// before `docker run` (image build, pull) lands here, not in the boot budget.
	startDeadline := time.Now().Add(fullnodeStartTimeout)
	for !fullnodeRunning() {
		select {
		case err := <-exited:
			return fmt.Errorf("make %s exited before %s was running: %v", target, fullnodeContainer, err)
		default:
		}
		if !time.Now().Before(startDeadline) {
			return fmt.Errorf("fullnode sidecar container didn't start within %s", fullnodeStartTimeout)
		}
		time.Sleep(fullnodeBootPoll)
	}

	// Phase 2: the node's own boot.
	deadline := time.Now().Add(fullnodeBootTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			return fmt.Errorf("make %s exited while %s was booting: %v", target, fullnodeContainer, err)
		default:
		}
		if fullnodeRunning() && fullnodeEVMReady() {
			fmt.Println("fullnode sidecar is ready")
			return nil
		}
		time.Sleep(fullnodeBootPoll)
	}
	return fmt.Errorf("fullnode sidecar didn't come up within %s of the container starting", fullnodeBootTimeout)
}

func fullnodeRunning() bool {
	out, err := exec.Command("docker", "ps",
		"--filter", "name="+fullnodeContainer,
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == fullnodeContainer
}

// fullnodeEVMReady reports whether the sidecar answers on the EVM-only RPC,
// which serves eth_getBalance, eth_getTransactionReceipt and
// eth_sendRawTransaction and nothing else.
func fullnodeEVMReady() bool {
	r, err := evmRPCInContainer(fullnodeContainer, "eth_getBalance", []any{fullnodeProbeAddress, "latest"})
	return err == nil && r.Error == nil && len(r.Result) > 0
}

type evmRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *evmRPCError    `json:"error,omitempty"`
}

type evmRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// evmRPCInContainer POSTs a JSON-RPC call to the given container's
// localhost:8545. The fullnode container's 8545 isn't host-published; this
// is the only way to talk to it without changing the run target.
func evmRPCInContainer(container, method string, params any) (*evmRPCResponse, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	out, err := exec.Command("docker", "exec", container,
		"curl", "-sf", "-X", "POST",
		"-H", "content-type: application/json",
		"--data", string(body),
		evmRPCURLOnContainerLocalhost).Output()
	if err != nil {
		return nil, fmt.Errorf("docker exec curl: %v", err)
	}
	var r evmRPCResponse
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("decode (body=%s): %w", out, err)
	}
	return &r, nil
}

// teardownCluster tears down every container TestMain brought up: first
// the fullnode sidecar (so its run-rpc-node `docker run --rm` process
// exits cleanly), then the validator cluster. Best-effort — errors are
// ignored so a partially-failed setupCluster can still clean up. Adding
// new sidecars later goes here too.
func teardownCluster() {
	fmt.Println("=== Stopping fullnode sidecar ===")
	_ = runMake(nil, "kill-rpc-node")
	fmt.Println("=== Stopping cluster ===")
	_ = runMake(nil, "docker-cluster-stop")
}

// countLaunchComplete returns the number of non-empty lines in the launch
// marker file (one per node). Returns 0 if the file does not exist.
func countLaunchComplete(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.TrimSpace(s.Text()) != "" {
			n++
		}
	}
	return n
}

func TestAutobahn(t *testing.T) {
	names := listRunningNodes(t)
	if len(names) == 0 {
		t.Fatalf("no running sei-node-* containers")
	}
	clusterSize = len(names)
	// BFT tolerates f faults in a cluster of n = 3f + 1 assuming equal
	// validator weights.
	// TODO: derive from stake weights once autobahn supports non-uniform
	// validator sets.
	maxFaults = (clusterSize - 1) / 3
	t.Logf("cluster size = %d, max tolerated faults = %d (assuming equal weights)", clusterSize, maxFaults)

	// EVMOnlyLoad needs every validator, so it runs first. The fault
	// subtests leave validators dead behind them and run in order:
	// HaltsBeyondMaxFaults kills one node past the set LivenessUnderMaxFaults
	// already killed.
	t.Run("EVMOnlyLoad", testEVMOnlyLoad)
	t.Run("LivenessUnderMaxFaults", testLivenessUnderMaxFaults)
	t.Run("HaltsBeyondMaxFaults", testHaltsBeyondMaxFaults)
	t.Run("Recovery", testRecovery)
}

// clusterHeight returns the highest execution height any running validator
// reports. A validator whose seid was killed stops serving metrics and drops
// out of the sample; the read fails only when no validator answers at all.
func clusterHeight(t *testing.T) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), heightReadTimeout)
	defer cancel()
	height := int64(-1)
	for _, container := range listRunningNodes(t) {
		executed, _, err := evmOnlyExecutionProgress(ctx, container)
		if err != nil {
			continue
		}
		height = max(height, executed)
	}
	if height < 0 {
		t.Fatalf("no validator reported an execution height")
	}
	return height
}

// waitForStableHeight returns the height once it has stayed constant for at
// least window. Used after killing validators: the cluster stops accepting new
// blocks immediately, but blocks already in flight keep draining through
// runExecute for a bounded but per-run variable time, so a halt is only
// observable as height standing still.
func waitForStableHeight(t *testing.T, window, timeout time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	h := clusterHeight(t)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		if time.Since(stableSince) >= window {
			return h
		}
		time.Sleep(heightPoll)
		nh := clusterHeight(t)
		if nh != h {
			h = nh
			stableSince = time.Now()
		}
	}
	t.Fatalf("height did not stabilize within %s (last seen %d)", timeout, h)
	return 0
}

// killNode kills seid inside sei-node-<i> via pkill. Tolerates non-zero exit
// (e.g. the process already gone).
func killNode(t *testing.T, i int) {
	t.Helper()
	t.Logf("killing seid on node %d...", i)
	_ = exec.Command("docker", "exec", fmt.Sprintf("sei-node-%d", i), "sh", "-c", "pkill seid").Run()
}

// restartNode re-invokes the container's seid-start script inside sei-node-<i>.
// The script backgrounds seid and exits, so `docker exec -d` is the right mode:
// it returns immediately while seid keeps running.
//
// Precondition: seid must NOT already be running on the target. start_sei.sh
// unconditionally spawns a new seid process; calling this while one is alive
// produces two seid instances in the same container (port/CMS-lock conflict).
// Callers should killNode first.
func restartNode(t *testing.T, i int) {
	t.Helper()
	t.Logf("restarting seid on node %d...", i)
	name := fmt.Sprintf("sei-node-%d", i)
	cmd := exec.Command("docker", "exec", "-d",
		"-e", fmt.Sprintf("ID=%d", i),
		name, "/usr/bin/start_sei.sh")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restartNode %d: %v\n%s", i, err, out)
	}
}

func evmOnlyTransferConfig(txs int) scenarios.Config {
	return scenarios.Config{
		TxsPerBlock:   txs,
		ChainID:       new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID),
		GasPrice:      big.NewInt(1_000_000_000),
		SenderBalance: new(big.Int).Lsh(big.NewInt(1), 200),
		TransferValue: big.NewInt(1),
		TxGasLimit:    21_000,
	}
}

// buildProgressTx returns one raw transfer from a sender that EVMOnlyLoad
// never used. SameSender plus a high block number picks DeterministicPrivateKey
// at progressTxAccountBase+seq, nonce 0.
func buildProgressTx(t *testing.T) []byte {
	t.Helper()
	cfg := evmOnlyTransferConfig(1)
	cfg.SameSender = true
	workload, err := scenarios.NewTransferWorkload(cfg, evmOnlyLoadState{})
	if err != nil {
		t.Fatalf("create progress-tx workload: %v", err)
	}
	block, err := workload.BuildBlock(t.Context(), progressTxAccountBase+progressTxSeq.Add(1))
	if err != nil {
		t.Fatalf("build progress tx: %v", err)
	}
	if len(block.Txs) != 1 {
		t.Fatalf("progress tx workload returned %d txs, want 1", len(block.Txs))
	}
	return block.Txs[0]
}

func sendEvmTx(t *testing.T, container string) (common.Hash, []byte) {
	t.Helper()
	raw := buildProgressTx(t)
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		t.Fatalf("decode progress tx: %v", err)
	}
	response, err := evmRPCInContainer(container, "eth_sendRawTransaction", []any{hexutil.Encode(raw)})
	if err != nil {
		t.Fatalf("send progress tx to %s: %v", container, err)
	}
	if response.Error != nil {
		t.Fatalf("send progress tx to %s: rpc %d %s", container, response.Error.Code, response.Error.Message)
	}
	return tx.Hash(), raw
}

// sendEvmTxAndWait submits a raw EVM-only transfer through container and waits
// until the fullnode has a receipt. That is the liveness signal: height can
// sit still under allow_empty_blocks=false until a tx seals a block.
func sendEvmTxAndWait(t *testing.T, container string, timeout time.Duration) int64 {
	t.Helper()
	base := clusterHeight(t)
	hash, raw := sendEvmTx(t, container)
	waitForEVMReceipt(t, container, hash, timeout)
	assertFullnodeExecutedTx(t, raw)
	height := clusterHeight(t)
	if height <= base {
		t.Fatalf("expected tx %s to land after height %d, last height %d", hash, base, height)
	}
	return height
}

// sendEvmTxExpectNoInclusion submits a tx after quorum loss. Height must stay
// at baseHeight and neither the validator nor the fullnode may serve a receipt.
func sendEvmTxExpectNoInclusion(t *testing.T, container string, baseHeight int64) {
	t.Helper()
	hash, _ := sendEvmTx(t, container)
	hAfter := waitForStableHeight(t, haltStableWindow, haltStableTimeout)
	if hAfter != baseHeight {
		t.Fatalf("expected no inclusion after quorum loss, but height advanced from %d to %d", baseHeight, hAfter)
	}
	if evmReceiptPresent(container, hash) {
		t.Fatalf("expected no inclusion after quorum loss, but %s has a receipt for %s", container, hash)
	}
	if evmReceiptPresent(fullnodeContainer, hash) {
		t.Fatalf("expected no inclusion after quorum loss, but fullnode has a receipt for %s", hash)
	}
	t.Logf("height stayed at %d after submitted tx %s", hAfter, hash)
}

func evmReceiptPresent(container string, hash common.Hash) bool {
	response, err := evmRPCInContainer(container, "eth_getTransactionReceipt", []any{hash})
	return err == nil && response.Error == nil && len(response.Result) > 0 && string(response.Result) != "null"
}

func waitForEVMReceipt(t *testing.T, container string, hash common.Hash, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if evmReceiptPresent(container, hash) {
			return
		}
		time.Sleep(fullnodeReceiptPoll)
	}
	t.Fatalf("%s served no receipt for %s within %s", container, hash, timeout)
}

// testLivenessUnderMaxFaults kills f = maxFaults validators (from the highest
// index downward). With clusterSize - f = 2f + 1 honest validators left, a
// submitted transaction must still finalize.
func testLivenessUnderMaxFaults(t *testing.T) {
	assertAutobahnEnabled(t)
	before := clusterHeight(t)
	t.Logf("height before: %d (killing %d validator(s), expecting a committed tx)", before, maxFaults)
	for i := 0; i < maxFaults; i++ {
		killNode(t, clusterSize-1-i)
	}
	t.Logf("height after: %d", sendEvmTxAndWait(t, "sei-node-0", livenessTimeout))
}

// testHaltsBeyondMaxFaults kills one validator beyond maxFaults, relying on
// LivenessUnderMaxFaults having killed the first maxFaults. Quorum is lost, so
// a submitted transaction must not finalize.
func testHaltsBeyondMaxFaults(t *testing.T) {
	assertAutobahnEnabled(t)
	killNode(t, clusterSize-1-maxFaults)
	halted := waitForStableHeight(t, haltStableWindow, haltStableTimeout)
	t.Logf("height: %d (expecting halt)", halted)
	sendEvmTxExpectNoInclusion(t, "sei-node-0", halted)
}

// testRecovery establishes its own halted precondition, then restarts one
// validator — the fault count returns to maxFaults, quorum is restored, and
// the chain must resume. Exercises the autobahn restart path (handshaker
// skipped, runExecute resumes from app.Info().LastBlockHeight).
//
// Self-contained: killNode is idempotent, so this works whether run in
// isolation or after LivenessUnderMaxFaults / HaltsBeyondMaxFaults.
func testRecovery(t *testing.T) {
	// TODO(autobahn): re-enable once the durable EVM-only execution cursor
	// (sei-protocol/sei-chain#4231, on giga-1) reaches main. Without it
	// evmOnlyApplication keeps committedHeight in memory only, so a restarted
	// validator reports height 0, re-runs InitChain, and replays block 1 onto
	// FlatKV state that is already ahead — it panics with "nonce too low"
	// instead of rejoining, and quorum never returns.
	t.Skip("EVM-only validators cannot restart until the durable execution cursor lands on main")

	assertAutobahnEnabled(t)
	for i := 0; i <= maxFaults; i++ {
		killNode(t, clusterSize-1-i)
	}
	halted := waitForStableHeight(t, haltStableWindow, haltStableTimeout)
	t.Logf("chain halted at height %d; restarting one validator", halted)

	restartNode(t, clusterSize-1-maxFaults)
	t.Logf("height after restart: %d", sendEvmTxAndWait(t, "sei-node-0", recoveryTimeout))

	// assertAutobahnEnabled greps every running container's log. The restarted
	// node is among them, and start_sei.sh truncates its log on restart (`>`
	// not `>>`), so the match on that one container necessarily comes from a
	// post-restart GigaRouter init — i.e., the restart reached giga setup.
	assertAutobahnEnabled(t)
}

type evmOnlyLoadState struct{}

func (evmOnlyLoadState) SetBalance(common.Address, *big.Int)               {}
func (evmOnlyLoadState) SetCode(common.Address, []byte)                    {}
func (evmOnlyLoadState) SetState(common.Address, common.Hash, common.Hash) {}

func testEVMOnlyLoad(t *testing.T) {
	assertAutobahnEnabled(t)
	assertEVMOnlyEnabled(t)
	assertTendermintRPCDisabled(t)
	if clusterSize != 4 {
		t.Fatalf("EVM-only Docker load test requires four validators, got %d", clusterSize)
	}

	workload, err := scenarios.NewTransferWorkload(evmOnlyTransferConfig(evmOnlyLoadTxs), evmOnlyLoadState{})
	if err != nil {
		t.Fatalf("create EVM-only transfer workload: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), evmOnlyLoadTimeout)
	defer cancel()
	block, err := workload.BuildBlock(ctx, 1)
	if err != nil {
		t.Fatalf("build EVM-only transfer workload: %v", err)
	}

	clients := make([]*ethrpc.Client, clusterSize)
	for i := range clients {
		client, err := ethrpc.DialContext(ctx, fmt.Sprintf("http://localhost:%d", 8545+2*i))
		if err != nil {
			t.Fatalf("create node %d EVM RPC client: %v", i, err)
		}
		clients[i] = client
		t.Cleanup(client.Close)
	}
	started := time.Now()
	group, groupCtx := errgroup.WithContext(ctx)
	for nodeIndex, client := range clients {
		nodeIndex, client := nodeIndex, client
		group.Go(func() error {
			for txIndex := nodeIndex; txIndex < len(block.Txs); txIndex += len(clients) {
				var hash common.Hash
				if err := client.CallContext(groupCtx, &hash, "eth_sendRawTransaction", hexutil.Bytes(block.Txs[txIndex])); err != nil {
					return fmt.Errorf("send raw transaction %d through node %d: %w", txIndex, nodeIndex, err)
				}
				if want := crypto.Keccak256Hash(block.Txs[txIndex]); hash != want {
					return fmt.Errorf("send raw transaction %d through node %d: hash %s, want %s", txIndex, nodeIndex, hash, want)
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		t.Fatal(err)
	}

	lastHeight, included := waitForEVMOnlyTxs(t, ctx, listRunningNodes(t), len(block.Txs))
	assertEVMOnlyReceipts(t, ctx, clients, block.Txs)
	assertEVMOnlyBalances(t, ctx, clients, block.Txs)
	assertEVMOnlyTransactionCount(t, ctx, clients, block.Txs)
	assertEVMOnlyChainID(t, ctx, clients)
	assertEVMOnlyBlockNumber(t, ctx, clients, lastHeight)
	assertFullnodeExecutedTx(t, block.Txs[0])
	elapsed := time.Since(started)
	t.Logf("Autobahn finalized %d raw EVM transfers through %d validators in %s (%.0f tx/s)",
		included, clusterSize, elapsed.Round(time.Millisecond), float64(included)/elapsed.Seconds())
	t.Logf("all validators executed through at least height %d", lastHeight)
}

func assertEVMOnlyBalances(t *testing.T, ctx context.Context, clients []*ethrpc.Client, txs [][]byte) {
	t.Helper()
	want := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 200), big.NewInt(1))
	for nodeIndex, client := range clients {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[nodeIndex]); err != nil {
			t.Fatalf("decode EVM-only transaction %d: %v", nodeIndex, err)
		}
		var got hexutil.Big
		if err := client.CallContext(ctx, &got, "eth_getBalance", tx.To(), "latest"); err != nil {
			t.Fatalf("read EVM-only balance %s from node %d: %v", tx.To(), nodeIndex, err)
		}
		if got.ToInt().Cmp(want) != 0 {
			t.Fatalf("node %d returned balance %s for %s, want %s", nodeIndex, got.ToInt(), tx.To(), want)
		}
	}
}

// assertEVMOnlyTransactionCount checks eth_getTransactionCount against every
// node: each transfer's sender starts at nonce 0 and sends exactly one
// transaction, so its committed nonce should now be 1.
func assertEVMOnlyTransactionCount(t *testing.T, ctx context.Context, clients []*ethrpc.Client, txs [][]byte) {
	t.Helper()
	signer := ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID))
	for nodeIndex, client := range clients {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[nodeIndex]); err != nil {
			t.Fatalf("decode EVM-only transaction %d: %v", nodeIndex, err)
		}
		sender, err := ethtypes.Sender(signer, tx)
		if err != nil {
			t.Fatalf("recover EVM-only sender for transaction %d: %v", nodeIndex, err)
		}
		var got hexutil.Uint64
		if err := client.CallContext(ctx, &got, "eth_getTransactionCount", sender, "latest"); err != nil {
			t.Fatalf("read EVM-only transaction count %s from node %d: %v", sender, nodeIndex, err)
		}
		if got != 1 {
			t.Fatalf("node %d returned transaction count %d for %s, want 1", nodeIndex, got, sender)
		}
	}
}

// assertEVMOnlyChainID checks eth_chainId against every node.
func assertEVMOnlyChainID(t *testing.T, ctx context.Context, clients []*ethrpc.Client) {
	t.Helper()
	wantChainID := new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID)
	for nodeIndex, client := range clients {
		var chainID hexutil.Big
		if err := client.CallContext(ctx, &chainID, "eth_chainId"); err != nil {
			t.Fatalf("read EVM-only chain ID from node %d: %v", nodeIndex, err)
		}
		if (*big.Int)(&chainID).Cmp(wantChainID) != 0 {
			t.Fatalf("node %d returned chain ID %s, want %s", nodeIndex, (*big.Int)(&chainID), wantChainID)
		}
	}
}

// assertEVMOnlyBlockNumber checks eth_blockNumber against every node once the
// load run has finalized through minHeight.
func assertEVMOnlyBlockNumber(t *testing.T, ctx context.Context, clients []*ethrpc.Client, minHeight int64) {
	t.Helper()
	for nodeIndex, client := range clients {
		var height hexutil.Uint64
		if err := client.CallContext(ctx, &height, "eth_blockNumber"); err != nil {
			t.Fatalf("read EVM-only block number from node %d: %v", nodeIndex, err)
		}
		if int64(height) < minHeight {
			t.Fatalf("node %d returned block number %d, want at least %d", nodeIndex, height, minHeight)
		}
	}
}

func assertEVMOnlyReceipts(t *testing.T, ctx context.Context, clients []*ethrpc.Client, txs [][]byte) {
	t.Helper()
	for nodeIndex, client := range clients {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[nodeIndex]); err != nil {
			t.Fatalf("decode EVM-only transaction %d: %v", nodeIndex, err)
		}
		txHash := tx.Hash()
		var got *struct {
			BlockHash        common.Hash     `json:"blockHash"`
			BlockNumber      hexutil.Uint64  `json:"blockNumber"`
			GasUsed          hexutil.Uint64  `json:"gasUsed"`
			Status           hexutil.Uint64  `json:"status"`
			To               *common.Address `json:"to"`
			TransactionHash  common.Hash     `json:"transactionHash"`
			TransactionIndex hexutil.Uint64  `json:"transactionIndex"`
		}
		if err := client.CallContext(ctx, &got, "eth_getTransactionReceipt", txHash); err != nil {
			t.Fatalf("read EVM-only receipt %s from node %d: %v", txHash, nodeIndex, err)
		}
		if got == nil {
			t.Fatalf("node %d returned null for finalized EVM-only receipt %s", nodeIndex, txHash)
		}
		if got.BlockHash == (common.Hash{}) {
			t.Fatalf("node %d returned an empty block hash for receipt %s", nodeIndex, txHash)
		}
		if got.BlockNumber == 0 {
			t.Fatalf("node %d returned block zero for receipt %s", nodeIndex, txHash)
		}
		if got.GasUsed != hexutil.Uint64(21_000) {
			t.Fatalf("node %d returned gasUsed %d for receipt %s", nodeIndex, got.GasUsed, txHash)
		}
		if got.Status != hexutil.Uint64(ethtypes.ReceiptStatusSuccessful) {
			t.Fatalf("node %d returned status %d for receipt %s", nodeIndex, got.Status, txHash)
		}
		if got.To == nil || tx.To() == nil || *got.To != *tx.To() {
			t.Fatalf("node %d returned to %v for receipt %s", nodeIndex, got.To, txHash)
		}
		if got.TransactionHash != txHash {
			t.Fatalf("node %d returned transaction hash %s, want %s", nodeIndex, got.TransactionHash, txHash)
		}
	}
}

// assertFullnodeExecutedTx requires the fullnode sidecar to serve a receipt for
// raw. The sidecar proposes nothing, so a receipt there is the observable proof
// that Autobahn's fullnode role pulled the committee's blocks and executed
// them. It trails the validators, hence the poll.
func assertFullnodeExecutedTx(t *testing.T, raw []byte) {
	t.Helper()
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		t.Fatalf("decode EVM-only transaction: %v", err)
	}
	waitForEVMReceipt(t, fullnodeContainer, tx.Hash(), fullnodeReceiptTimeout)
	t.Logf("fullnode %s executed %s", fullnodeContainer, tx.Hash())
}

// assertTendermintRPCDisabled checks that no validator serves Tendermint RPC:
// Autobahn serves the EVM JSON-RPC only.
func assertTendermintRPCDisabled(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	for i := range clusterSize {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", 26657+3*i))
		if err != nil {
			continue
		}
		_ = response.Body.Close()
		t.Fatalf("node %d unexpectedly serves Tendermint RPC with HTTP status %s", i, response.Status)
	}
}

func waitForEVMOnlyTxs(t *testing.T, ctx context.Context, containers []string, target int) (int64, int) {
	t.Helper()
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("waiting for %d EVM-only transactions on every validator: %v", target, err)
		}
		minHeight := int64(^uint64(0) >> 1)
		minExecuted := target
		allExecuted := true
		for _, container := range containers {
			height, executed, err := evmOnlyExecutionProgress(ctx, container)
			if err != nil || executed < target {
				allExecuted = false
				break
			}
			minHeight = min(minHeight, height)
			minExecuted = min(minExecuted, executed)
		}
		if allExecuted {
			return minHeight, minExecuted
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func evmOnlyExecutionProgress(ctx context.Context, container string) (int64, int, error) {
	out, err := exec.CommandContext(ctx, "docker", "exec", container, "curl", "-fsS", evmOnlyMetricsURL).Output()
	if err != nil {
		return 0, 0, err
	}
	nextBlock, ok := prometheusSample(out, evmOnlyNextBlock, `stage="execute"`)
	if !ok || nextBlock < 1 {
		return 0, 0, fmt.Errorf("missing EVM-only execution height metric")
	}
	executed, ok := prometheusSample(out, evmOnlyTxLatency, `resource="txs"`, `stage="execute"`)
	if !ok {
		return 0, 0, fmt.Errorf("missing EVM-only executed transaction metric")
	}
	return int64(nextBlock) - 1, int(executed), nil
}

func prometheusSample(metrics []byte, name string, labels ...string) (float64, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(metrics)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		matches := true
		for _, label := range labels {
			if !strings.Contains(line, label) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
