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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
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
	// setupTimeout bounds TestMain's setup, which runs outside the go test timeout.
	setupTimeout = 30 * time.Minute

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
	// prebuiltImagesEnv selects the -ci make targets, which reuse the prebuilt
	// sei-chain/localnode and sei-chain/rpcnode images and build/seid.
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
	seidExitTimeout   = 30 * time.Second
	seidExitPoll      = 200 * time.Millisecond

	evmOnlyLoadTxs     = 4_000
	evmOnlyLoadTimeout = 3 * time.Minute
	evmOnlyMetricsURL  = "http://127.0.0.1:26660/metrics"
	evmOnlyNextBlock   = "tendermint_internal_autobahn_data_next_block"
	evmOnlyTxLatency   = "tendermint_internal_autobahn_data_latency_count"

	// RPC query checks that run after EVMOnlyLoad's transfers execute.
	evmOnlyRPCTimeout = 3 * time.Minute
	evmOnlyRPCPoll    = 200 * time.Millisecond
	// evmOnlyContractDeployerAccount is past the load and progress-tx senders,
	// so the contract deploy is the first transaction from its account.
	evmOnlyContractDeployerAccount = 2_000_000
	evmOnlyContractGasLimit        = 200_000
	evmOnlyTransferGas             = 21_000
)

var (
	// evmOnlyContractRuntime returns storage slot 0 as one 32-byte word:
	// PUSH1 0 SLOAD PUSH1 0 MSTORE PUSH1 32 PUSH1 0 RETURN.
	evmOnlyContractRuntime = []byte{0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	// evmOnlyContractInitCode stores evmOnlyContractSlotValue at slot 0, emits
	// one log with topic evmOnlyContractLogTopic and no data, then deploys
	// evmOnlyContractRuntime.
	evmOnlyContractInitCode = append([]byte{
		0x60, 0x2a, 0x60, 0x00, 0x55, // SSTORE(0, 0x2a)
		0x60, 0x01, 0x60, 0x00, 0x60, 0x00, 0xa1, // LOG1(0, 0, topic 1)
		0x60, 0x0b, 0x60, 0x18, 0x60, 0x00, 0x39, // CODECOPY(0, 24, 11)
		0x60, 0x0b, 0x60, 0x00, 0xf3, // RETURN(0, 11)
	}, evmOnlyContractRuntime...)
	evmOnlyContractSlotValue = common.BigToHash(big.NewInt(0x2a))
	evmOnlyContractLogTopic  = common.BigToHash(big.NewInt(1))
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

// TestMain brings up the autobahn docker cluster and the fullnode sidecar
// before the tests run and tears them down afterward. The working directory
// is changed to the repo root so the `make docker-cluster-*` targets resolve
// their relative paths.
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
	if err := setup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		teardownCluster()
		os.Exit(1)
	}
	code := m.Run()
	teardownCluster()
	os.Exit(code)
}

// findRepoRoot walks up from the current working directory looking for the
// first directory containing a go.mod.
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

// setup brings up the validator cluster and the fullnode sidecar, failing
// if they are not both ready within setupTimeout.
func setup() error {
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	if err := setupCluster(ctx); err != nil {
		return fmt.Errorf("cluster setup failed: %w", err)
	}
	if err := setupFullnodeNode(ctx); err != nil {
		return fmt.Errorf("fullnode sidecar setup failed: %w", err)
	}
	return nil
}

// runMake runs `make <target>` from the current directory, streaming output.
// The process and its children are killed when ctx ends.
func runMake(ctx context.Context, env []string, target string) error {
	cmd := exec.CommandContext(ctx, "make", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	return err
}

// pause waits for d, returning ctx's error if ctx ends first.
func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// setupCluster starts the autobahn docker cluster and waits until all nodes
// have signalled readiness via build/generated/launch.complete.
func setupCluster(ctx context.Context) error {
	fmt.Println("=== Starting Autobahn Integration Tests ===")
	// Best-effort cleanup of any prior cluster, then wipe generated state.
	_ = runMake(ctx, nil, "docker-cluster-stop")
	if err := os.RemoveAll("build/generated"); err != nil {
		return fmt.Errorf("rm -rf build/generated: %w", err)
	}
	target := "docker-cluster-start"
	if prebuiltImages() {
		target += "-ci"
	}
	if err := runMake(ctx, []string{"AUTOBAHN=true", "DOCKER_DETACH=true"}, target); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	// Count created containers (they exist immediately post-compose-up) to
	// determine how many launch.complete entries to wait for.
	expected, err := countSeiContainers(ctx)
	if err != nil {
		return fmt.Errorf("count cluster containers: %w", err)
	}
	if expected == 0 {
		return fmt.Errorf("no sei-node-* containers found after %s", target)
	}
	fmt.Printf("Waiting for %d nodes to be ready...\n", expected)
	deadline := time.Now().Add(clusterBootTimeout)
	for time.Now().Before(deadline) {
		if n := countLaunchComplete("build/generated/launch.complete"); n >= expected {
			fmt.Printf("All %d nodes are ready\n", expected)
			fmt.Printf("Waiting %s for autobahn connections to establish...\n", autobahnSettleDelay)
			return pause(ctx, autobahnSettleDelay)
		}
		if err := pause(ctx, clusterBootPoll); err != nil {
			return fmt.Errorf("waiting for nodes to be ready: %w", err)
		}
	}
	return fmt.Errorf("cluster failed to start within %s", clusterBootTimeout)
}

// countSeiContainers returns the number of sei-node-* containers that exist
// (running or not yet started).
func countSeiContainers(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
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
func setupFullnodeNode(ctx context.Context) error {
	fmt.Println("=== Starting fullnode sidecar ===")
	_ = runMake(ctx, nil, "kill-rpc-node") // best-effort cleanup

	// Discover the cluster size from docker so the rpc-node's autobahn config
	// covers exactly the validators that came up — non-four-node test runs
	// would otherwise produce a mismatched committee.
	clusterSize, err := countSeiContainers(ctx)
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
	for !fullnodeRunning(ctx) {
		select {
		case err := <-exited:
			return fmt.Errorf("make %s exited before %s was running: %v", target, fullnodeContainer, err)
		default:
		}
		if !time.Now().Before(startDeadline) {
			return fmt.Errorf("fullnode sidecar container didn't start within %s", fullnodeStartTimeout)
		}
		if err := pause(ctx, fullnodeBootPoll); err != nil {
			return fmt.Errorf("waiting for %s to start: %w", fullnodeContainer, err)
		}
	}

	// Phase 2: the node's own boot.
	deadline := time.Now().Add(fullnodeBootTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			return fmt.Errorf("make %s exited while %s was booting: %v", target, fullnodeContainer, err)
		default:
		}
		if fullnodeRunning(ctx) && fullnodeEVMReady() {
			fmt.Println("fullnode sidecar is ready")
			return nil
		}
		if err := pause(ctx, fullnodeBootPoll); err != nil {
			return fmt.Errorf("waiting for %s to boot: %w", fullnodeContainer, err)
		}
	}
	return fmt.Errorf("fullnode sidecar didn't come up within %s of the container starting", fullnodeBootTimeout)
}

func fullnodeRunning(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "docker", "ps",
		"--filter", "name="+fullnodeContainer,
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == fullnodeContainer
}

// fullnodeEVMReady reports whether the sidecar answers eth_getBalance on the
// EVM-only RPC.
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
		"curl", "-sf", "--max-time", "10", "-X", "POST",
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
	_ = runMake(context.Background(), nil, "kill-rpc-node")
	fmt.Println("=== Stopping cluster ===")
	_ = runMake(context.Background(), nil, "docker-cluster-stop")
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

// killNode kills seid inside sei-node-<i> via pkill and waits until the
// process is gone. Tolerates pkill non-zero (already dead). Restarting
// before that wait would spawn a second seid in the same container.
func killNode(t *testing.T, i int) {
	t.Helper()
	name := fmt.Sprintf("sei-node-%d", i)
	t.Logf("killing seid on node %d...", i)
	_ = exec.Command("docker", "exec", name, "sh", "-c", "pkill seid").Run()
	deadline := time.Now().Add(seidExitTimeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("docker", "exec", name, "sh", "-c", "pgrep seid").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return
		}
		time.Sleep(seidExitPoll)
	}
	t.Fatalf("seid still running on %s after pkill (%s)", name, seidExitTimeout)
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

	rpcCtx, rpcCancel := context.WithTimeout(t.Context(), evmOnlyRPCTimeout)
	defer rpcCancel()
	assertEVMOnlyBlockQueries(t, rpcCtx, clients, block.Txs[0])
	assertEVMOnlyNodeInfo(t, rpcCtx, clients)
	assertEVMOnlyContractQueries(t, rpcCtx, clients)
	assertEVMOnlyNewHeads(t, rpcCtx, clients)
	assertEVMOnlyTransactionErrors(t, rpcCtx, clients, block.Txs[0])
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

// evmOnlyBlockSummary is the part of an eth_getBlockBy* response, with
// transaction hashes only, that the RPC query checks compare.
type evmOnlyBlockSummary struct {
	Hash         common.Hash    `json:"hash"`
	Number       hexutil.Uint64 `json:"number"`
	GasLimit     hexutil.Uint64 `json:"gasLimit"`
	GasUsed      hexutil.Uint64 `json:"gasUsed"`
	Transactions []common.Hash  `json:"transactions"`
}

// evmOnlyReceiptSummary is the part of a receipt that the RPC query checks
// compare.
type evmOnlyReceiptSummary struct {
	BlockHash        common.Hash       `json:"blockHash"`
	BlockNumber      hexutil.Uint64    `json:"blockNumber"`
	ContractAddress  *common.Address   `json:"contractAddress"`
	GasUsed          hexutil.Uint64    `json:"gasUsed"`
	Logs             []json.RawMessage `json:"logs"`
	Status           hexutil.Uint64    `json:"status"`
	TransactionHash  common.Hash       `json:"transactionHash"`
	TransactionIndex hexutil.Uint64    `json:"transactionIndex"`
}

// evmOnlyCall calls method on node nodeIndex and returns its raw JSON result.
func evmOnlyCall(t *testing.T, ctx context.Context, client *ethrpc.Client, nodeIndex int, method string, args ...any) json.RawMessage {
	t.Helper()
	var result json.RawMessage
	if err := client.CallContext(ctx, &result, method, args...); err != nil {
		t.Fatalf("node %d %s: %v", nodeIndex, method, err)
	}
	return result
}

// decodeEVMOnlyResult decodes method's raw result from node nodeIndex into v.
func decodeEVMOnlyResult(t *testing.T, nodeIndex int, method string, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("node %d %s: decode %s: %v", nodeIndex, method, raw, err)
	}
}

// requireSameJSON fails unless a and b encode the same JSON value.
func requireSameJSON(t *testing.T, what string, a, b json.RawMessage) {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("%s: decode %s: %v", what, a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("%s: decode %s: %v", what, b, err)
	}
	if !reflect.DeepEqual(av, bv) {
		t.Fatalf("%s differ:\n%s\n%s", what, a, b)
	}
}

// waitForEVMOnlyResult polls method on every node until it answers non-null,
// returning node 0's answer.
func waitForEVMOnlyResult(t *testing.T, ctx context.Context, clients []*ethrpc.Client, method string, args ...any) json.RawMessage {
	t.Helper()
	var first json.RawMessage
	for nodeIndex, client := range clients {
		for {
			raw := evmOnlyCall(t, ctx, client, nodeIndex, method, args...)
			if string(raw) != "null" {
				if nodeIndex == 0 {
					first = raw
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatalf("node %d %s%v stayed null: %v", nodeIndex, method, args, ctx.Err())
			case <-time.After(evmOnlyRPCPoll):
			}
		}
	}
	return first
}

// assertEVMOnlyBlockQueries checks every block-scoped method against the block
// that executed raw, on every node: the block lists raw at its receipt's index,
// lookups by number, by hash, and by index agree with each other and with
// eth_getTransactionByHash and eth_getTransactionReceipt, the block's receipts
// account for its gasUsed, eth_feeHistory reports that same block, and every
// node returns the identical block and receipts.
func assertEVMOnlyBlockQueries(t *testing.T, ctx context.Context, clients []*ethrpc.Client, raw []byte) {
	t.Helper()
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		t.Fatalf("decode EVM-only transaction: %v", err)
	}
	txHash := tx.Hash()
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID)), tx)
	if err != nil {
		t.Fatalf("recover EVM-only sender: %v", err)
	}
	var receipt evmOnlyReceiptSummary
	decodeEVMOnlyResult(t, 0, "eth_getTransactionReceipt", evmOnlyCall(t, ctx, clients[0], 0, "eth_getTransactionReceipt", txHash), &receipt)
	height, blockHash, index := receipt.BlockNumber, receipt.BlockHash, int(receipt.TransactionIndex)
	// A block's receipts, and the gasUsed derived from them, are served once
	// they are indexed, which can trail the receipt lookup by hash.
	waitForEVMOnlyResult(t, ctx, clients, "eth_getBlockReceipts", height)
	gasPrice := evmOnlyTransferConfig(1).GasPrice

	var firstBlock, firstReceipts json.RawMessage
	for nodeIndex, client := range clients {
		call := func(method string, args ...any) json.RawMessage {
			return evmOnlyCall(t, ctx, client, nodeIndex, method, args...)
		}

		hashOnly := call("eth_getBlockByNumber", height, false)
		var block evmOnlyBlockSummary
		decodeEVMOnlyResult(t, nodeIndex, "eth_getBlockByNumber", hashOnly, &block)
		if block.Hash != blockHash || block.Number != height {
			t.Fatalf("node %d block %d has hash %s number %d, want %s %d", nodeIndex, height, block.Hash, block.Number, blockHash, height)
		}
		if index >= len(block.Transactions) || block.Transactions[index] != txHash {
			t.Fatalf("node %d block %d does not list %s at receipt index %d: %v", nodeIndex, height, txHash, index, block.Transactions)
		}
		requireSameJSON(t, fmt.Sprintf("node %d eth_getBlockByNumber and eth_getBlockByHash", nodeIndex),
			hashOnly, call("eth_getBlockByHash", blockHash, false))

		full := call("eth_getBlockByNumber", height, true)
		requireSameJSON(t, fmt.Sprintf("node %d full eth_getBlockByNumber and eth_getBlockByHash", nodeIndex),
			full, call("eth_getBlockByHash", blockHash, true))
		var fullTxs struct {
			Transactions []json.RawMessage `json:"transactions"`
		}
		decodeEVMOnlyResult(t, nodeIndex, "eth_getBlockByNumber", full, &fullTxs)
		if len(fullTxs.Transactions) != len(block.Transactions) {
			t.Fatalf("node %d block %d lists %d full transactions and %d hashes", nodeIndex, height, len(fullTxs.Transactions), len(block.Transactions))
		}
		for i, rawTx := range fullTxs.Transactions {
			var fullTx struct {
				Hash common.Hash `json:"hash"`
			}
			decodeEVMOnlyResult(t, nodeIndex, "eth_getBlockByNumber", rawTx, &fullTx)
			if fullTx.Hash != block.Transactions[i] {
				t.Fatalf("node %d block %d full transaction %d is %s, hash list has %s", nodeIndex, height, i, fullTx.Hash, block.Transactions[i])
			}
		}

		byHash := call("eth_getTransactionByHash", txHash)
		var located struct {
			BlockHash        common.Hash    `json:"blockHash"`
			BlockNumber      hexutil.Uint64 `json:"blockNumber"`
			From             common.Address `json:"from"`
			Hash             common.Hash    `json:"hash"`
			TransactionIndex hexutil.Uint64 `json:"transactionIndex"`
		}
		decodeEVMOnlyResult(t, nodeIndex, "eth_getTransactionByHash", byHash, &located)
		if located.Hash != txHash || located.From != sender || located.BlockHash != blockHash ||
			located.BlockNumber != height || int(located.TransactionIndex) != index {
			t.Fatalf("node %d eth_getTransactionByHash(%s) = %s, want block %s/%d index %d from %s",
				nodeIndex, txHash, byHash, blockHash, height, index, sender)
		}
		requireSameJSON(t, fmt.Sprintf("node %d full block transaction and eth_getTransactionByHash", nodeIndex),
			fullTxs.Transactions[index], byHash)
		requireSameJSON(t, fmt.Sprintf("node %d eth_getTransactionByBlockNumberAndIndex and eth_getTransactionByHash", nodeIndex),
			call("eth_getTransactionByBlockNumberAndIndex", height, hexutil.Uint(index)), byHash)
		requireSameJSON(t, fmt.Sprintf("node %d eth_getTransactionByBlockHashAndIndex and eth_getTransactionByHash", nodeIndex),
			call("eth_getTransactionByBlockHashAndIndex", blockHash, hexutil.Uint(index)), byHash)
		if past := call("eth_getTransactionByBlockNumberAndIndex", height, hexutil.Uint(len(block.Transactions))); string(past) != "null" {
			t.Fatalf("node %d returned %s for the index past block %d's last transaction, want null", nodeIndex, past, height)
		}

		for _, count := range []struct {
			method string
			block  any
		}{
			{"eth_getBlockTransactionCountByNumber", height},
			{"eth_getBlockTransactionCountByHash", blockHash},
		} {
			var got hexutil.Uint
			decodeEVMOnlyResult(t, nodeIndex, count.method, call(count.method, count.block), &got)
			if int(got) != len(block.Transactions) {
				t.Fatalf("node %d %s(%v) = %d, block lists %d transactions", nodeIndex, count.method, count.block, got, len(block.Transactions))
			}
		}

		receiptsRaw := call("eth_getBlockReceipts", height)
		requireSameJSON(t, fmt.Sprintf("node %d eth_getBlockReceipts by number and by hash", nodeIndex),
			receiptsRaw, call("eth_getBlockReceipts", blockHash))
		var receipts []evmOnlyReceiptSummary
		var receiptsJSON []json.RawMessage
		decodeEVMOnlyResult(t, nodeIndex, "eth_getBlockReceipts", receiptsRaw, &receipts)
		decodeEVMOnlyResult(t, nodeIndex, "eth_getBlockReceipts", receiptsRaw, &receiptsJSON)
		var gasUsed uint64
		previous, found := -1, false
		for i, r := range receipts {
			ri := int(r.TransactionIndex)
			if r.BlockHash != blockHash || r.BlockNumber != height || ri <= previous || ri >= len(block.Transactions) ||
				block.Transactions[ri] != r.TransactionHash {
				t.Fatalf("node %d block %d receipt %d (%s at index %d) does not match the block's transaction list", nodeIndex, height, i, r.TransactionHash, ri)
			}
			if r.Status != hexutil.Uint64(ethtypes.ReceiptStatusSuccessful) || r.GasUsed != evmOnlyTransferGas {
				t.Fatalf("node %d block %d receipt for %s has status %d gasUsed %d, want a successful transfer", nodeIndex, height, r.TransactionHash, r.Status, r.GasUsed)
			}
			previous = ri
			gasUsed += uint64(r.GasUsed)
			if r.TransactionHash == txHash {
				found = true
				requireSameJSON(t, fmt.Sprintf("node %d eth_getBlockReceipts entry and eth_getTransactionReceipt", nodeIndex),
					receiptsJSON[i], call("eth_getTransactionReceipt", txHash))
			}
		}
		if !found {
			t.Fatalf("node %d eth_getBlockReceipts(%d) omits %s", nodeIndex, height, txHash)
		}
		if uint64(block.GasUsed) != gasUsed {
			t.Fatalf("node %d block %d reports gasUsed %d, its receipts sum to %d", nodeIndex, height, block.GasUsed, gasUsed)
		}

		var logs []json.RawMessage
		logsRaw := call("eth_getLogs", map[string]any{"fromBlock": height, "toBlock": height})
		decodeEVMOnlyResult(t, nodeIndex, "eth_getLogs", logsRaw, &logs)
		if logs == nil || len(logs) != 0 {
			t.Fatalf("node %d eth_getLogs over transfer block %d = %s, want []", nodeIndex, height, logsRaw)
		}

		var history struct {
			OldestBlock  hexutil.Uint64   `json:"oldestBlock"`
			BaseFee      []*hexutil.Big   `json:"baseFeePerGas"`
			GasUsedRatio []float64        `json:"gasUsedRatio"`
			Reward       [][]*hexutil.Big `json:"reward"`
		}
		decodeEVMOnlyResult(t, nodeIndex, "eth_feeHistory", call("eth_feeHistory", hexutil.Uint64(1), height, []float64{50}), &history)
		// gasUsedRatio is truncated to four decimal places, and zero without a gas limit.
		wantRatio := 0.0
		if block.GasLimit > 0 {
			wantRatio = float64(uint64(block.GasUsed)*10000/uint64(block.GasLimit)) / 10000
		}
		if history.OldestBlock != height || len(history.GasUsedRatio) != 1 || history.GasUsedRatio[0] != wantRatio {
			t.Fatalf("node %d eth_feeHistory(1, %d) = oldest %d ratios %v, want oldest %d ratio %v", nodeIndex, height, history.OldestBlock, history.GasUsedRatio, height, wantRatio)
		}
		// Every transfer in the block paid gasPrice over a zero base fee;
		// baseFeePerGas also carries the next block's projected fee.
		if len(history.BaseFee) != 2 || history.BaseFee[0].ToInt().Sign() != 0 || history.BaseFee[1].ToInt().Sign() != 0 ||
			len(history.Reward) != 1 || len(history.Reward[0]) != 1 || history.Reward[0][0].ToInt().Cmp(gasPrice) != 0 {
			t.Fatalf("node %d eth_feeHistory(1, %d) base fees %v rewards %v, want a zero base fee and a %s reward", nodeIndex, height, history.BaseFee, history.Reward, gasPrice)
		}

		if nodeIndex == 0 {
			firstBlock, firstReceipts = full, receiptsRaw
			continue
		}
		requireSameJSON(t, fmt.Sprintf("block %d on node 0 and node %d", height, nodeIndex), firstBlock, full)
		requireSameJSON(t, fmt.Sprintf("block %d receipts on node 0 and node %d", height, nodeIndex), firstReceipts, receiptsRaw)
	}
}

// assertEVMOnlyNodeInfo checks the node-level methods on every node: the
// network version is the chain ID, eth_syncing is unsupported as on v2, and
// the suggested prices clear the price the load paid.
func assertEVMOnlyNodeInfo(t *testing.T, ctx context.Context, clients []*ethrpc.Client) {
	t.Helper()
	wantVersion := strconv.FormatUint(tmconfig.AutobahnEVMOnlyChainID, 10)
	gasPrice := evmOnlyTransferConfig(1).GasPrice
	for nodeIndex, client := range clients {
		var version string
		decodeEVMOnlyResult(t, nodeIndex, "net_version", evmOnlyCall(t, ctx, client, nodeIndex, "net_version"), &version)
		if version != wantVersion {
			t.Fatalf("node %d net_version = %q, want %q", nodeIndex, version, wantVersion)
		}

		var syncing json.RawMessage
		if err := client.CallContext(ctx, &syncing, "eth_syncing"); err == nil || !strings.Contains(err.Error(), "eth_syncing is not supported") {
			t.Fatalf("node %d eth_syncing = %s, %v; want the not-supported error", nodeIndex, syncing, err)
		}

		var suggested hexutil.Big
		decodeEVMOnlyResult(t, nodeIndex, "eth_gasPrice", evmOnlyCall(t, ctx, client, nodeIndex, "eth_gasPrice"), &suggested)
		if suggested.ToInt().Cmp(gasPrice) < 0 {
			t.Fatalf("node %d eth_gasPrice = %s, below the %s the load was admitted at", nodeIndex, suggested.ToInt(), gasPrice)
		}
		var tip hexutil.Big
		decodeEVMOnlyResult(t, nodeIndex, "eth_maxPriorityFeePerGas", evmOnlyCall(t, ctx, client, nodeIndex, "eth_maxPriorityFeePerGas"), &tip)
		if tip.ToInt().Sign() <= 0 {
			t.Fatalf("node %d eth_maxPriorityFeePerGas = %s, want a positive tip", nodeIndex, tip.ToInt())
		}
	}
}

// assertEVMOnlyContractQueries deploys evmOnlyContractInitCode and checks the
// state and log methods against what its constructor did, on every node: the
// deployed code, the stored slot, a call that reads it back, gas estimates
// that cover the deploy and the call, and the constructor's log through both
// the receipt and eth_getLogs.
func assertEVMOnlyContractQueries(t *testing.T, ctx context.Context, clients []*ethrpc.Client) {
	t.Helper()
	key, err := scenarios.DeterministicPrivateKey(evmOnlyContractDeployerAccount)
	if err != nil {
		t.Fatalf("derive contract deployer key: %v", err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	var nonce hexutil.Uint64
	decodeEVMOnlyResult(t, 0, "eth_getTransactionCount",
		evmOnlyCall(t, ctx, clients[0], 0, "eth_getTransactionCount", sender, "latest"), &nonce)
	contract := crypto.CreateAddress(sender, uint64(nonce))

	var deployEstimate hexutil.Uint64
	decodeEVMOnlyResult(t, 0, "eth_estimateGas", evmOnlyCall(t, ctx, clients[0], 0, "eth_estimateGas",
		map[string]any{"from": sender, "data": hexutil.Bytes(evmOnlyContractInitCode)}), &deployEstimate)
	if deployEstimate <= 53_000 || deployEstimate > evmOnlyContractGasLimit {
		t.Fatalf("eth_estimateGas for the contract deploy = %d, want above the 53000 creation floor and at most %d", deployEstimate, evmOnlyContractGasLimit)
	}

	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    uint64(nonce),
		GasPrice: evmOnlyTransferConfig(1).GasPrice,
		Gas:      evmOnlyContractGasLimit,
		Data:     evmOnlyContractInitCode,
	}), ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID)), key)
	if err != nil {
		t.Fatalf("sign contract deploy: %v", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("encode contract deploy: %v", err)
	}
	var sent common.Hash
	if err := clients[0].CallContext(ctx, &sent, "eth_sendRawTransaction", hexutil.Bytes(raw)); err != nil {
		t.Fatalf("send contract deploy: %v", err)
	}
	if sent != signed.Hash() {
		t.Fatalf("send contract deploy: hash %s, want %s", sent, signed.Hash())
	}

	receiptRaw := waitForEVMOnlyResult(t, ctx, clients, "eth_getTransactionReceipt", signed.Hash())
	var receipt evmOnlyReceiptSummary
	decodeEVMOnlyResult(t, 0, "eth_getTransactionReceipt", receiptRaw, &receipt)
	if receipt.Status != hexutil.Uint64(ethtypes.ReceiptStatusSuccessful) || receipt.ContractAddress == nil ||
		*receipt.ContractAddress != contract || uint64(receipt.GasUsed) > uint64(deployEstimate) || len(receipt.Logs) != 1 {
		t.Fatalf("contract deploy receipt %s, want a successful deploy of %s using at most the %d estimate with one log", receiptRaw, contract, deployEstimate)
	}
	var log struct {
		Address         common.Address `json:"address"`
		Topics          []common.Hash  `json:"topics"`
		Data            hexutil.Bytes  `json:"data"`
		BlockHash       common.Hash    `json:"blockHash"`
		TransactionHash common.Hash    `json:"transactionHash"`
	}
	decodeEVMOnlyResult(t, 0, "eth_getTransactionReceipt", receipt.Logs[0], &log)
	if log.Address != contract || len(log.Topics) != 1 || log.Topics[0] != evmOnlyContractLogTopic ||
		len(log.Data) != 0 || log.BlockHash != receipt.BlockHash || log.TransactionHash != signed.Hash() {
		t.Fatalf("contract deploy log %s, want the constructor's log from %s", receipt.Logs[0], contract)
	}
	wantLogs, err := json.Marshal(receipt.Logs)
	if err != nil {
		t.Fatalf("encode contract deploy logs: %v", err)
	}
	waitForEVMOnlyResult(t, ctx, clients, "eth_getBlockReceipts", receipt.BlockNumber)

	for nodeIndex, client := range clients {
		call := func(method string, args ...any) json.RawMessage {
			return evmOnlyCall(t, ctx, client, nodeIndex, method, args...)
		}
		requireSameJSON(t, fmt.Sprintf("contract deploy receipt on node 0 and node %d", nodeIndex),
			receiptRaw, call("eth_getTransactionReceipt", signed.Hash()))

		var code hexutil.Bytes
		decodeEVMOnlyResult(t, nodeIndex, "eth_getCode", call("eth_getCode", contract, "latest"), &code)
		if !bytes.Equal(code, evmOnlyContractRuntime) {
			t.Fatalf("node %d eth_getCode(%s) = %s, want %s", nodeIndex, contract, code, hexutil.Bytes(evmOnlyContractRuntime))
		}
		decodeEVMOnlyResult(t, nodeIndex, "eth_getCode", call("eth_getCode", sender, "latest"), &code)
		if len(code) != 0 {
			t.Fatalf("node %d eth_getCode(%s) = %s for an account without code, want 0x", nodeIndex, sender, code)
		}

		for _, slot := range []struct {
			key  string
			want common.Hash
		}{
			{"0x0", evmOnlyContractSlotValue},
			{"0x1", common.Hash{}},
		} {
			var value hexutil.Bytes
			decodeEVMOnlyResult(t, nodeIndex, "eth_getStorageAt", call("eth_getStorageAt", contract, slot.key, "latest"), &value)
			if !bytes.Equal(value, slot.want[:]) {
				t.Fatalf("node %d eth_getStorageAt(%s, %s) = %s, want %s", nodeIndex, contract, slot.key, value, slot.want)
			}
		}

		var result hexutil.Bytes
		decodeEVMOnlyResult(t, nodeIndex, "eth_call", call("eth_call", map[string]any{"to": contract}, "latest"), &result)
		if !bytes.Equal(result, evmOnlyContractSlotValue[:]) {
			t.Fatalf("node %d eth_call(%s) = %s, want slot 0's %s", nodeIndex, contract, result, evmOnlyContractSlotValue)
		}

		var transferEstimate, callEstimate hexutil.Uint64
		decodeEVMOnlyResult(t, nodeIndex, "eth_estimateGas", call("eth_estimateGas",
			map[string]any{"from": sender, "to": common.Address{0x01}, "value": (*hexutil.Big)(big.NewInt(1))}), &transferEstimate)
		if transferEstimate != evmOnlyTransferGas {
			t.Fatalf("node %d eth_estimateGas for a transfer = %d, want %d", nodeIndex, transferEstimate, evmOnlyTransferGas)
		}
		decodeEVMOnlyResult(t, nodeIndex, "eth_estimateGas", call("eth_estimateGas",
			map[string]any{"from": sender, "to": contract}), &callEstimate)
		if callEstimate <= evmOnlyTransferGas {
			t.Fatalf("node %d eth_estimateGas for a call reading storage = %d, want more than %d", nodeIndex, callEstimate, evmOnlyTransferGas)
		}

		requireSameJSON(t, fmt.Sprintf("node %d eth_getLogs over the deploy block and the deploy receipt's logs", nodeIndex),
			call("eth_getLogs", map[string]any{"fromBlock": receipt.BlockNumber, "toBlock": receipt.BlockNumber}), wantLogs)
		requireSameJSON(t, fmt.Sprintf("node %d eth_getLogs by block hash and the deploy receipt's logs", nodeIndex),
			call("eth_getLogs", map[string]any{"blockHash": receipt.BlockHash, "address": contract}), wantLogs)
		var unmatched []json.RawMessage
		decodeEVMOnlyResult(t, nodeIndex, "eth_getLogs", call("eth_getLogs", map[string]any{
			"blockHash": receipt.BlockHash, "topics": []common.Hash{common.BigToHash(big.NewInt(2))},
		}), &unmatched)
		if len(unmatched) != 0 {
			t.Fatalf("node %d eth_getLogs with a topic no log carries returned %d logs", nodeIndex, len(unmatched))
		}
	}
}

// assertEVMOnlyNewHeads subscribes to newHeads over every node's WebSocket
// listener, seals a block with one transfer, and checks that each node pushes
// a head that matches the block it serves at that height.
func assertEVMOnlyNewHeads(t *testing.T, ctx context.Context, clients []*ethrpc.Client) {
	t.Helper()
	type head struct {
		Hash   common.Hash    `json:"hash"`
		Number hexutil.Uint64 `json:"number"`
	}
	heads := make([]chan head, len(clients))
	subs := make([]*ethrpc.ClientSubscription, len(clients))
	for nodeIndex := range clients {
		ws, err := ethrpc.DialContext(ctx, fmt.Sprintf("ws://localhost:%d", 8546+2*nodeIndex))
		if err != nil {
			t.Fatalf("create node %d EVM WebSocket client: %v", nodeIndex, err)
		}
		t.Cleanup(ws.Close)
		heads[nodeIndex] = make(chan head, 16)
		subs[nodeIndex], err = ws.EthSubscribe(ctx, heads[nodeIndex], "newHeads")
		if err != nil {
			t.Fatalf("node %d eth_subscribe(newHeads): %v", nodeIndex, err)
		}
		t.Cleanup(subs[nodeIndex].Unsubscribe)
	}
	// Empty blocks are not produced, so a transfer is what seals the next head.
	sendEvmTx(t, "sei-node-0")
	for nodeIndex, client := range clients {
		select {
		case got := <-heads[nodeIndex]:
			var block evmOnlyBlockSummary
			decodeEVMOnlyResult(t, nodeIndex, "eth_getBlockByNumber",
				evmOnlyCall(t, ctx, client, nodeIndex, "eth_getBlockByNumber", got.Number, false), &block)
			if got.Number == 0 || got.Hash != block.Hash {
				t.Fatalf("node %d pushed head %d %s, it serves block %d as %s", nodeIndex, got.Number, got.Hash, got.Number, block.Hash)
			}
		case err := <-subs[nodeIndex].Err():
			t.Fatalf("node %d newHeads subscription ended: %v", nodeIndex, err)
		case <-ctx.Done():
			t.Fatalf("node %d pushed no head: %v", nodeIndex, ctx.Err())
		}
	}
}

// evmOnlyFailingDeployerAccount is past the contract deployer, so the failing
// deploy is the first transaction from its account.
const evmOnlyFailingDeployerAccount = 2_000_001

// assertEVMOnlyTransactionErrors sends a deploy whose init code is the INVALID
// opcode and checks, on every node, that eth_getVMError and
// eth_getTransactionErrorByHash report its error and report "" for the
// successful transfer succeeded, that an unknown hash is "" for
// eth_getTransactionErrorByHash and an error for eth_getVMError, and that
// web3_clientVersion answers in the regular RPC's Geth format.
func assertEVMOnlyTransactionErrors(t *testing.T, ctx context.Context, clients []*ethrpc.Client, succeeded []byte) {
	t.Helper()
	key, err := scenarios.DeterministicPrivateKey(evmOnlyFailingDeployerAccount)
	if err != nil {
		t.Fatalf("derive failing deployer key: %v", err)
	}
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		GasPrice: evmOnlyTransferConfig(1).GasPrice,
		Gas:      evmOnlyContractGasLimit,
		Data:     []byte{0xfe},
	}), ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(tmconfig.AutobahnEVMOnlyChainID)), key)
	if err != nil {
		t.Fatalf("sign failing deploy: %v", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("encode failing deploy: %v", err)
	}
	if err := clients[0].CallContext(ctx, nil, "eth_sendRawTransaction", hexutil.Bytes(raw)); err != nil {
		t.Fatalf("send failing deploy: %v", err)
	}
	var receipt evmOnlyReceiptSummary
	decodeEVMOnlyResult(t, 0, "eth_getTransactionReceipt",
		waitForEVMOnlyResult(t, ctx, clients, "eth_getTransactionReceipt", signed.Hash()), &receipt)
	if receipt.Status != hexutil.Uint64(ethtypes.ReceiptStatusFailed) {
		t.Fatalf("failing deploy %s has status %d, want failed", signed.Hash(), receipt.Status)
	}
	successful := new(ethtypes.Transaction)
	if err := successful.UnmarshalBinary(succeeded); err != nil {
		t.Fatalf("decode successful transfer: %v", err)
	}

	var wantVMError string
	for nodeIndex, client := range clients {
		var vmError, errorByHash string
		decodeEVMOnlyResult(t, nodeIndex, "eth_getVMError", evmOnlyCall(t, ctx, client, nodeIndex, "eth_getVMError", signed.Hash()), &vmError)
		decodeEVMOnlyResult(t, nodeIndex, "eth_getTransactionErrorByHash",
			evmOnlyCall(t, ctx, client, nodeIndex, "eth_getTransactionErrorByHash", signed.Hash()), &errorByHash)
		if nodeIndex == 0 {
			wantVMError = vmError
		}
		if vmError == "" || vmError != wantVMError || errorByHash != vmError {
			t.Fatalf("node %d failing deploy: eth_getVMError = %q, eth_getTransactionErrorByHash = %q, want node 0's non-empty %q from both",
				nodeIndex, vmError, errorByHash, wantVMError)
		}

		for _, method := range []string{"eth_getVMError", "eth_getTransactionErrorByHash"} {
			var got string
			decodeEVMOnlyResult(t, nodeIndex, method, evmOnlyCall(t, ctx, client, nodeIndex, method, successful.Hash()), &got)
			if got != "" {
				t.Fatalf("node %d %s(%s) = %q for a successful transfer, want \"\"", nodeIndex, method, successful.Hash(), got)
			}
		}

		var unknown string
		decodeEVMOnlyResult(t, nodeIndex, "eth_getTransactionErrorByHash",
			evmOnlyCall(t, ctx, client, nodeIndex, "eth_getTransactionErrorByHash", common.Hash{}), &unknown)
		if unknown != "" {
			t.Fatalf("node %d eth_getTransactionErrorByHash for an unknown hash = %q, want \"\"", nodeIndex, unknown)
		}
		if err := client.CallContext(ctx, &unknown, "eth_getVMError", common.Hash{}); err == nil || !strings.Contains(err.Error(), "receipt not found") {
			t.Fatalf("node %d eth_getVMError for an unknown hash = %q, %v; want the receipt-not-found error", nodeIndex, unknown, err)
		}

		var version string
		decodeEVMOnlyResult(t, nodeIndex, "web3_clientVersion", evmOnlyCall(t, ctx, client, nodeIndex, "web3_clientVersion"), &version)
		if !strings.HasPrefix(version, "Geth/") {
			t.Fatalf("node %d web3_clientVersion = %q, want the regular RPC's Geth/ form", nodeIndex, version)
		}
	}
}
