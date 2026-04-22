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

	tmjson "github.com/sei-protocol/sei-chain/sei-tendermint/libs/json"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

const (
	abciInfoURL   = "http://localhost:26657/abci_info"
	heightRetries = 60
	heightBackoff = 500 * time.Millisecond
	heightTimeout = 100 * time.Millisecond

	// Cluster lifecycle (TestMain).
	clusterBootTimeout  = 5 * time.Minute
	clusterBootPoll     = 5 * time.Second
	autobahnSettleDelay = 30 * time.Second
)

var heightClient = &http.Client{Timeout: heightTimeout}

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

// getHeight reads last_block_height from /abci_info and retries until the
// chain has produced at least one block (height > 0). ABCI returns 0 between
// InitChain and the first FinalizeBlock; we treat that as "not ready" since
// all callers assume a live, advancing chain.
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

// teardownCluster runs `make docker-cluster-stop`, ignoring errors.
func teardownCluster() {
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
	t.Run("BankTransfer", testBankTransfer)
	t.Run("LivenessUnderMaxFaults", testLivenessUnderMaxFaults)
	t.Run("HaltsBeyondMaxFaults", testHaltsBeyondMaxFaults)
	t.Run("Recovery", testRecovery)
}

// restartNode re-invokes the container's seid-start script inside sei-node-<i>.
// The script backgrounds seid and exits, so `docker exec -d` is the right mode:
// it returns immediately while seid keeps running.
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

// testRecovery: HaltsBeyondMaxFaults left the chain halted with maxFaults+1
// nodes killed. Restart one of them — fault count returns to maxFaults, quorum
// is restored, chain should resume. This exercises the autobahn restart path
// (handshaker skipped, runExecute resumes from app.Info().LastBlockHeight).
func testRecovery(t *testing.T) {
	hBefore := getHeight(t)
	t.Logf("height entering Recovery: %d (expected halted)", hBefore)

	// Restart the node HaltsBeyondMaxFaults killed last.
	target := clusterSize - 1 - maxFaults
	restartNode(t, target)

	// Poll for the chain to advance. Give the restarted seid time to init
	// and rejoin consensus.
	deadline := time.Now().Add(90 * time.Second)
	var hAfter int64
	for time.Now().Before(deadline) {
		hAfter = getHeight(t)
		if hAfter > hBefore {
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("height after restart: %d", hAfter)
	if hAfter <= hBefore {
		t.Fatalf("chain did not resume advancing after restart of node %d (%d -> %d)",
			target, hBefore, hAfter)
	}

	// Log on the restarted node should now contain a fresh GigaRouter init.
	assertAutobahnEnabled(t)
}

func testBlockProduction(t *testing.T) {
	assertAutobahnEnabled(t)
	h1 := getHeight(t)
	t.Logf("height: %d", h1)
	time.Sleep(5 * time.Second)
	h2 := getHeight(t)
	t.Logf("height after 5s: %d", h2)
	if h2 <= h1 {
		t.Fatalf("block height not advancing (%d -> %d)", h1, h2)
	}
}

func testBankTransfer(t *testing.T) {
	assertAutobahnEnabled(t)

	// Create recipient. stderr is redirected inside the container so stdout is pure JSON.
	createOut := dockerExec(t, "sei-node-0",
		"printf '12345678\n12345678\n' | seid keys add test_recipient --output json 2>/dev/null")
	var key struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(createOut)), &key); err != nil {
		t.Fatalf("parse recipient address: %v\noutput: %s", err, createOut)
	}
	t.Logf("recipient: %s", key.Address)

	// Send from node_admin (genesis account) to recipient.
	// Use -b sync (not -b block) because CometBFT consensus is disabled in autobahn mode.
	// TODO: support -b block once autobahn supports it.
	sendCmd := fmt.Sprintf(
		"printf '12345678\n' | seid tx bank send node_admin %s 1000000usei "+
			"--chain-id sei --fees 2000usei -b sync -y --output json",
		key.Address)
	dockerExec(t, "sei-node-0", sendCmd)

	// Poll for balance. Tolerate transient query failures before the tx finalizes.
	t.Log("waiting for tx to finalize...")
	queryCmd := fmt.Sprintf("seid q bank balances %s --denom usei --output json 2>/dev/null", key.Address)
	var balance string
	for attempt := 0; attempt < 15; attempt++ {
		out, _ := exec.Command("docker", "exec", "sei-node-0", "sh", "-c", queryCmd).Output()
		var b struct {
			Amount string `json:"amount"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &b); err == nil {
			balance = b.Amount
			if balance == "1000000" {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("balance: %s usei", balance)
	if balance != "1000000" {
		t.Fatalf("expected balance 1000000, got %s", balance)
	}
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
func testLivenessUnderMaxFaults(t *testing.T) {
	assertAutobahnEnabled(t)
	hBefore := getHeight(t)
	t.Logf("height before: %d (killing %d node(s), expecting progress)", hBefore, maxFaults)
	for i := 0; i < maxFaults; i++ {
		killNode(t, clusterSize-1-i)
	}
	time.Sleep(10 * time.Second)
	hAfter := getHeight(t)
	t.Logf("height after: %d", hAfter)
	if hAfter <= hBefore {
		t.Fatalf("chain should continue with %d/%d validators (%d -> %d)",
			clusterSize-maxFaults, clusterSize, hBefore, hAfter)
	}
}

// testHaltsBeyondMaxFaults kills one more node beyond maxFaults (relies on the
// prior LivenessUnderMaxFaults having already killed the first maxFaults). The
// chain should stop advancing.
func testHaltsBeyondMaxFaults(t *testing.T) {
	assertAutobahnEnabled(t)
	killNode(t, clusterSize-1-maxFaults)
	time.Sleep(5 * time.Second)
	hBefore := getHeight(t)
	t.Logf("height: %d (expecting halt)", hBefore)
	time.Sleep(15 * time.Second)
	hAfter := getHeight(t)
	t.Logf("height after 15s: %d", hAfter)
	if hAfter != hBefore {
		t.Fatalf("chain should halt with %d/%d validators (height changed: %d -> %d)",
			clusterSize-maxFaults-1, clusterSize, hBefore, hAfter)
	}
}
