//go:build autobahn_integration

// Package autobahn contains integration tests for the autobahn consensus mode.
//
// Requires a running autobahn Docker cluster. Run via:
//
//	make autobahn-integration-test
//
// Or directly (cluster must already be up):
//
//	go test -tags autobahn_integration -v ./integration_test/autobahn/...
package autobahn

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/integration_test/internal/evmtest"
	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

const (
	// tmRPCBase points at the fullnode sidecar's CometBFT RPC (port 26657
	// inside the container, host-published at 26669 via the rpc-node's
	// docker run port mapping). The whole test suite routes its RPC reads
	// through here — matches the production shape where clients talk to
	// fullnodes, not validators.
	tmRPCBase     = "http://localhost:26669"
	abciInfoURL   = tmRPCBase + "/abci_info"
	heightRetries = 60
	heightBackoff = 500 * time.Millisecond
	heightTimeout = 100 * time.Millisecond
	// tmRPCTimeout covers single-shot tmRPC verifications post-bootstrap.
	// Looser than heightTimeout (which is intentionally tight to keep
	// height-polling retries quick) because these calls happen on a chain
	// we've already confirmed is live.
	tmRPCTimeout = 5 * time.Second

	// Cluster lifecycle (TestMain).
	clusterBootTimeout  = 5 * time.Minute
	clusterBootPoll     = 5 * time.Second
	autobahnSettleDelay = 30 * time.Second

	// Fullnode sidecar lifecycle (TestMain).
	fullnodeContainer   = "sei-rpc-node"
	fullnodeBootTimeout = 5 * time.Minute
	fullnodeBootPoll    = 5 * time.Second
	// evmRPCURLOnContainerLocalhost is the EVM RPC address inside the
	// rpc-node container — used with `docker exec ... curl` for readiness
	// checks (the rpc-node's 8545 isn't host-published).
	evmRPCURLOnContainerLocalhost = "http://localhost:8545"

	// heightPoll governs waitForStableHeight: the fullnode's read of
	// /abci_info trails the cluster while runExecute drains buffered
	// blocks, and a killed-peer failover (DialInterval-bounded, ~10s)
	// holds height static for that long. Polling lets each test absorb
	// whatever combination of those delays actually applies, instead of
	// guessing a sleep duration.
	heightPoll       = 1 * time.Second
	haltStableWindow = 20 * time.Second
	// 2m / 90s give headroom for the fullnode catch-up backlog the
	// preceding subtest may have left (failover delay during
	// LivenessUnderMaxFaults can put the fullnode ~600 blocks behind,
	// which takes ~60s to drain on top of the halt-detection window).
	// CI runners are slower than local; 1m was tight enough to flake.
	haltStableTimeout = 2 * time.Minute
	testRecipientEVM  = "0x1000000000000000000000000000000000000001"
)

var (
	heightClient = &http.Client{Timeout: heightTimeout}
	tmRPCClient  = &http.Client{Timeout: tmRPCTimeout}
)

// clusterSize is set once at TestAutobahn start from the number of running
// sei-node-* containers. Subtests read it (and maxFaults) from here.
var (
	clusterSize int
	maxFaults   int
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

// getHeight reads last_block_height from /abci_info and retries until a
// non-zero committed height is observed.
//
// Uses abci_info instead of /status because /status reads from the CometBFT
// block store, which autobahn does not populate.
// TODO: switch back to /status once autobahn supports it.
func getHeight(t *testing.T) int64 {
	t.Helper()
	for i := 0; i < heightRetries; i++ {
		h, err := fetchHeight()
		if err == nil && h > 0 {
			return h
		}
		time.Sleep(heightBackoff)
	}
	t.Fatalf("could not get block height after %d retries", heightRetries)
	return 0
}

func currentHeight(t *testing.T) int64 {
	t.Helper()
	h, err := fetchHeight()
	if err != nil {
		t.Fatalf("fetch height: %v", err)
	}
	return h
}

// waitForStableHeight polls getHeight every heightPoll. It returns the
// height once the value has stayed constant for at least `window`. Useful
// after killing validators: cluster halt is observable through the rpc-
// only's read of /abci_info only once any in-flight blocks have drained
// through runExecute and any block-sync failover has finished — both
// bounded in absolute time but variable per run. Fails the test if no
// stable window appears within `timeout`.
func waitForStableHeight(t *testing.T, window, timeout time.Duration) int64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	h := getHeight(t)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		if time.Since(stableSince) >= window {
			return h
		}
		time.Sleep(heightPoll)
		nh := getHeight(t)
		if nh != h {
			h = nh
			stableSince = time.Now()
		}
	}
	t.Fatalf("height did not stabilize within %s (last seen %d)", timeout, h)
	return 0
}

func fetchHeight() (int64, error) {
	resp, err := heightClient.Get(abciInfoURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	// Use tmjson: tendermint's RPC encodes int64 as a JSON string, which
	// stdlib encoding/json can't decode into int64.
	var parsed coretypes.ResultABCIInfo
	if err := tmjson.Unmarshal(body, &parsed); err != nil {
		return 0, err
	}
	return parsed.Response.LastBlockHeight, nil
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

// dockerExec runs `docker exec <container> sh -c <script>` and returns stdout.
func dockerExec(t *testing.T, container, script string) string {
	t.Helper()
	cmd := exec.Command("docker", "exec", container, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec %s failed: %v\n%s", container, err, out)
	}
	return string(out)
}

// dockerExecAllowFail runs docker exec but doesn't fail the test on non-zero exit.
func dockerExecAllowFail(container, script string) {
	_ = exec.Command("docker", "exec", container, "sh", "-c", script).Run()
}

func waitForReceiptBlockNumber(t *testing.T, txHash string) int64 {
	t.Helper()
	for {
		if err := t.Context().Err(); err != nil {
			t.Fatalf("receipt for %s not observed before test context ended: %v", txHash, err)
		}
		resp, err := evmRPCInContainer(fullnodeContainer, "eth_getTransactionReceipt", []any{txHash})
		if err != nil {
			time.Sleep(heightPoll)
			continue
		}
		if resp.Error != nil {
			t.Fatalf("eth_getTransactionReceipt(%s): code=%d message=%s", txHash, resp.Error.Code, resp.Error.Message)
		}
		if string(resp.Result) == "null" || len(resp.Result) == 0 {
			time.Sleep(heightPoll)
			continue
		}

		var receipt struct {
			BlockNumber string `json:"blockNumber"`
			Status      string `json:"status"`
		}
		if err := json.Unmarshal(resp.Result, &receipt); err != nil {
			t.Fatalf("decode receipt for %s: %v\nbody: %s", txHash, err, resp.Result)
		}
		if receipt.Status != "" && receipt.Status != "0x1" {
			t.Fatalf("tx %s reverted with status %s", txHash, receipt.Status)
		}
		if receipt.BlockNumber == "" {
			time.Sleep(heightPoll)
			continue
		}

		var height int64
		if _, err := fmt.Sscanf(receipt.BlockNumber, "0x%x", &height); err != nil {
			t.Fatalf("parse receipt block number %q for %s: %v", receipt.BlockNumber, txHash, err)
		}
		return height
	}
}

func evmBalanceHex(t *testing.T, address string) string {
	t.Helper()
	resp, err := evmRPCInContainer(fullnodeContainer, "eth_getBalance", []any{address, "latest"})
	if err != nil {
		t.Fatalf("eth_getBalance(%s): %v", address, err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_getBalance(%s): code=%d message=%s", address, resp.Error.Code, resp.Error.Message)
	}
	var balance string
	if err := json.Unmarshal(resp.Result, &balance); err != nil {
		t.Fatalf("decode balance for %s: %v\nbody: %s", address, err, resp.Result)
	}
	return balance
}

func sendEvmTx(t *testing.T, container string) string {
	t.Helper()
	// Progress-only tx: these subtests use "a tx finalized in a new block" as
	// the observable signal that Autobahn is live or halted.
	txHash, err := evmtest.SendTinyEvmTx(t.Context(), evmtest.DockerTxConfig{
		Container: container,
		Password:  "12345678",
		From:      "node_admin",
		Recipient: testRecipientEVM,
		ChainID:   "sei",
		EVMRPCURL: evmRPCURLOnContainerLocalhost,
	})
	if err != nil {
		t.Fatalf("send evm tx: %v", err)
	}
	return txHash
}

func sendEvmTxAndWait(t *testing.T, container string) int64 {
	t.Helper()
	baseHeight := currentHeight(t)
	txHash := sendEvmTx(t, container)
	receiptHeight := waitForReceiptBlockNumber(t, txHash)
	if receiptHeight <= baseHeight {
		t.Fatalf("expected tx %s to land after height %d, got receipt at %d", txHash, baseHeight, receiptHeight)
	}
	return receiptHeight
}

func sendEvmTxExpectNoInclusion(t *testing.T, container string, baseHeight int64) {
	t.Helper()
	// Progress-only tx: after quorum loss, this should remain uncommitted and
	// height should stay fixed, proving that no new block can be produced.
	txHash := sendEvmTx(t, container)
	hAfter := waitForStableHeight(t, haltStableWindow, haltStableTimeout)
	if hAfter != baseHeight {
		t.Fatalf("expected no inclusion after quorum loss, but height advanced from %d to %d", baseHeight, hAfter)
	}
	resp, err := evmRPCInContainer(fullnodeContainer, "eth_getTransactionReceipt", []any{txHash})
	if err == nil && resp != nil && resp.Error == nil && string(resp.Result) != "null" && len(resp.Result) > 0 {
		t.Fatalf("expected no inclusion after quorum loss, but tx %s received receipt %s", txHash, resp.Result)
	}
	t.Logf("height stayed at %d after submitted tx", hAfter)
}

// TestMain brings up the autobahn docker cluster before the test runs and
// tears it down afterward. The working directory is changed to the repo root
// so the `make docker-cluster-*` targets resolve their relative paths.
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
		teardownCluster() // best-effort
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

// findRepoRoot walks up from the current working directory looking for the
// first directory containing a go.mod. Returns that directory.
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
	// Start cluster in the background (DOCKER_DETACH=true).
	if err := runMake([]string{"AUTOBAHN=true", "DOCKER_DETACH=true"}, "docker-cluster-start"); err != nil {
		return fmt.Errorf("docker-cluster-start: %w", err)
	}
	// Count created containers (they exist immediately post-compose-up) to
	// determine how many launch.complete entries to wait for.
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
	cmd := exec.Command("make", "run-rpc-node-skipbuild")
	cmd.Env = append(os.Environ(), "AUTOBAHN=true", fmt.Sprintf("CLUSTER_SIZE=%d", clusterSize))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start make run-rpc-node-skipbuild: %w", err)
	}
	// Reap the process when it eventually exits (e.g. on container kill);
	// not blocking on Wait here since the container runs for the duration
	// of the test suite.
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(fullnodeBootTimeout)
	for time.Now().Before(deadline) {
		if fullnodeRunning() && fullnodeEVMReady() {
			fmt.Println("fullnode sidecar is ready")
			return nil
		}
		time.Sleep(fullnodeBootPoll)
	}
	return fmt.Errorf("fullnode sidecar didn't come up within %s", fullnodeBootTimeout)
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

func fullnodeEVMReady() bool {
	r, err := evmRPCInContainer(fullnodeContainer, "eth_chainId", []any{})
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
	// Discover cluster size once, before any test kills nodes.
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

	t.Run("BlockProduction", testBlockProduction)
	t.Run("EVMTransfer", testEVMTransfer)
	t.Run("LivenessUnderMaxFaults", testLivenessUnderMaxFaults)
	t.Run("HaltsBeyondMaxFaults", testHaltsBeyondMaxFaults)
	t.Run("Recovery", testRecovery)
}

// restartNode re-invokes the container's seid-start script inside sei-node-<i>.
// The script backgrounds seid and exits, so `docker exec -d` is the right mode:
// it returns immediately while seid keeps running.
//
// Precondition: seid must NOT already be running on the target. start_sei.sh
// unconditionally spawns a new seid process; calling this while one is alive
// produces two seid instances in the same container (port/CMS-lock conflict).
// Callers should `killNode` first, or extend the script to pkill defensively.
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

// testRecovery establishes its own halted precondition, then restarts one
// node — fault count returns to maxFaults, quorum is restored, chain should
// resume. Exercises the autobahn restart path (handshaker skipped,
// runExecute resumes from app.Info().LastBlockHeight).
//
// Self-contained: does not rely on prior subtests. killNode is idempotent
// (pkill tolerates an already-dead process), so this works whether run in
// isolation or after LivenessUnderMaxFaults / HaltsBeyondMaxFaults.
func testRecovery(t *testing.T) {
	assertAutobahnEnabled(t)

	// Force the halted precondition: kill maxFaults+1 nodes. If earlier
	// subtests already killed some of these, those kills are no-ops.
	for i := 0; i <= maxFaults; i++ {
		killNode(t, clusterSize-1-i)
	}

	// Wait for the fullnode's view of height to stabilize (cluster halt +
	// fullnode drain + any failover from a killed peer). The window inside
	// waitForStableHeight already proves the chain isn't advancing.
	hBefore := waitForStableHeight(t, haltStableWindow, haltStableTimeout)
	if h := getHeight(t); h != hBefore {
		t.Fatalf("expected halted chain after killing %d nodes, but height advanced (%d -> %d)",
			maxFaults+1, hBefore, h)
	}
	t.Logf("chain halted at height %d; restarting one node", hBefore)

	// Restart one node to restore quorum.
	target := clusterSize - 1 - maxFaults
	restartNode(t, target)

	// A committed tx is the liveness signal here: once quorum is restored,
	// a new tx should finalize and advance height.
	hAfter := sendEvmTxAndWait(t, "sei-node-0")
	if hAfter <= hBefore {
		t.Fatalf("expected committed tx after recovery to advance height past %d, got %d", hBefore, hAfter)
	}
	t.Logf("height after restart: %d", hAfter)

	// assertAutobahnEnabled greps every running container's log. The restarted
	// node is among them, and start_sei.sh truncates its log on restart (`>`
	// not `>>`), so the match on that one container necessarily comes from a
	// post-restart GigaRouter init — i.e., the restart reached giga setup.
	assertAutobahnEnabled(t)
}

func testBlockProduction(t *testing.T) {
	assertAutobahnEnabled(t)
	h := sendEvmTxAndWait(t, "sei-node-0")
	t.Logf("height after committed evm tx: %d", h)

	// Verify the Autobahn-routed tmRPC handlers serve real data at h (a
	// recently committed height — past tail of the chain, so historical
	// query paths are exercised without racing the producer). Each
	// endpoint asserts one observable property; a single mismatch fails
	// the test with the specific shape that broke.
	assertTmRPCEndpoints(t, h)
}

// assertTmRPCEndpoints exercises the tmRPC surface that PR #3310 wires up
// under Autobahn (env.Block, env.BlockResults, env.BlockByHash, env.Validators).
// One call per endpoint is enough — these handlers are pure RPC translation
// over the same data.State / GenDoc plumbing, so a single positive case at
// a real height catches both wrong-routing (e.g. CometBFT path returning
// nulls because BlockStore is empty) and shape-drift regressions.
func assertTmRPCEndpoints(t *testing.T, h int64) {
	t.Helper()

	// /block at h: must return a fully-populated translated block.
	var rb coretypes.ResultBlock
	fetchTmRPC(t, fmt.Sprintf("%s/block?height=%d", tmRPCBase, h), &rb)
	if rb.Block == nil {
		t.Fatalf("/block?height=%d: nil block (env.Block likely fell through to empty BlockStore)", h)
	}
	if rb.Block.Height != h {
		t.Fatalf("/block?height=%d: got block.height=%d", h, rb.Block.Height)
	}
	if len(rb.BlockID.Hash) == 0 {
		t.Fatalf("/block?height=%d: empty BlockID.Hash (Autobahn header → CometBFT BlockID translation skipped)", h)
	}

	// /block_by_hash with the hash we just received: must round-trip to
	// the same height. Exercises GigaRouter's hash → height index in
	// data.State.inner.blockHashes. Note: use bare-hex form (no `0x`
	// prefix) — the 0x form goes through a binary-base64 round-trip in
	// the URI handler that doesn't cleanly traverse HexBytes.UnmarshalText
	// for our request shape; bare hex stays on the string path.
	var rbh coretypes.ResultBlock
	fetchTmRPC(t, fmt.Sprintf("%s/block_by_hash?hash=%x", tmRPCBase, rb.BlockID.Hash), &rbh)
	if rbh.Block == nil {
		t.Fatalf("/block_by_hash(%x): nil block (hash index miss)", rb.BlockID.Hash)
	}
	if rbh.Block.Height != h {
		t.Fatalf("/block_by_hash(%x): got height %d, want %d (round-trip mismatch)",
			rb.BlockID.Hash, rbh.Block.Height, h)
	}

	// /block_results at h: header echo. We don't assert TxsResults shape —
	// it's intentionally empty under Autobahn (FinalizeBlock responses
	// aren't persisted; documented in PR #3310).
	var rbr coretypes.ResultBlockResults
	fetchTmRPC(t, fmt.Sprintf("%s/block_results?height=%d", tmRPCBase, h), &rbr)
	if rbr.Height != h {
		t.Fatalf("/block_results?height=%d: got height=%d", h, rbr.Height)
	}

	// /validators at h: committee is fixed at genesis under Autobahn, so
	// any retained height returns it. block_height in the response must
	// match the requested height (catches the old "stuck at 1" StateStore
	// behavior).
	var rv coretypes.ResultValidators
	fetchTmRPC(t, fmt.Sprintf("%s/validators?height=%d", tmRPCBase, h), &rv)
	if rv.BlockHeight != h {
		t.Fatalf("/validators?height=%d: got block_height=%d (StateStore-stuck-at-1 regression?)",
			h, rv.BlockHeight)
	}
	if rv.Total < 1 || len(rv.Validators) < 1 {
		t.Fatalf("/validators?height=%d: empty committee (total=%d, count=%d)",
			h, rv.Total, len(rv.Validators))
	}
}

// fetchTmRPC issues a GET against a tmRPC URL-form endpoint and decodes the
// (unwrapped, non-JSONRPC) response into `into` via tmjson, which handles the
// int-as-string convention CometBFT uses on the wire. Mirrors fetchHeight's
// shape but with the looser tmRPCTimeout for one-shot verifications.
//
// Detects server-side errors before unmarshaling: tmRPC URL-form returns
// either the result struct directly (success) or a {code,message,data}
// object (error). Without this check, an error response would silently
// unmarshal into a zero-valued result struct because none of the keys
// match, causing tests to read "missing field" as "data missing" rather
// than "the call failed".
func fetchTmRPC[T any](t *testing.T, url string, into *T) {
	t.Helper()
	resp, err := tmRPCClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	var maybeErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if json.Unmarshal(body, &maybeErr) == nil && maybeErr.Code != 0 {
		t.Fatalf("GET %s: server error code=%d message=%q data=%q",
			url, maybeErr.Code, maybeErr.Message, maybeErr.Data)
	}
	if err := tmjson.Unmarshal(body, into); err != nil {
		t.Fatalf("parse %s: %v\nbody: %s", url, err, body)
	}
}

func testEVMTransfer(t *testing.T) {
	assertAutobahnEnabled(t)
	before := evmBalanceHex(t, testRecipientEVM)
	h := sendEvmTxAndWait(t, "sei-node-0")
	after := evmBalanceHex(t, testRecipientEVM)
	if before == after {
		t.Fatalf("expected recipient %s balance to change after evm tx at height %d", testRecipientEVM, h)
	}
	t.Logf("evm transfer committed at height %d (balance %s -> %s)", h, before, after)
}

// killNode kills seid inside sei-node-<i> via pkill. Tolerates non-zero exit
// (e.g. the process already gone).
func killNode(t *testing.T, i int) {
	t.Helper()
	t.Logf("killing seid on node %d...", i)
	dockerExecAllowFail(fmt.Sprintf("sei-node-%d", i), "pkill seid")
}

// testLivenessUnderMaxFaults kills f = maxFaults nodes (from the highest index
// downward). With clusterSize - f = 2f + 1 honest nodes left, the chain should
// still advance.
//
// Polls for height to advance (instead of a fixed sleep): if the fullnode
// happened to be subscribed to the killed peer, its block-sync subscriber
// pauses for DialInterval (~10s) before failing over, so height stays at
// hBefore until then.
func testLivenessUnderMaxFaults(t *testing.T) {
	assertAutobahnEnabled(t)
	hBefore := getHeight(t)
	t.Logf("height before: %d (killing %d node(s), expecting progress)", hBefore, maxFaults)
	for i := 0; i < maxFaults; i++ {
		killNode(t, clusterSize-1-i)
	}
	hAfter := sendEvmTxAndWait(t, "sei-node-0")
	if hAfter <= hBefore {
		t.Fatalf("expected committed tx with %d faults to advance height past %d, got %d", maxFaults, hBefore, hAfter)
	}
	t.Logf("height after: %d", hAfter)
}

// testHaltsBeyondMaxFaults kills one more node beyond maxFaults (relies on the
// prior LivenessUnderMaxFaults having already killed the first maxFaults). The
// chain should stop advancing.
//
// Reads come through the fullnode sidecar, which lags the cluster while it
// drains buffered blocks through runExecute (and longer when the killed
// peer was the one fullnode was subscribed to — failover sleeps
// DialInterval before retrying). Instead of guessing a fixed settle, we
// poll getHeight and only sample once the value has been stable for a
// short window.
func testHaltsBeyondMaxFaults(t *testing.T) {
	assertAutobahnEnabled(t)
	killNode(t, clusterSize-1-maxFaults)
	hBefore := waitForStableHeight(t, haltStableWindow, haltStableTimeout)
	t.Logf("height: %d (expecting halt)", hBefore)
	sendEvmTxExpectNoInclusion(t, "sei-node-0", hBefore)
}
