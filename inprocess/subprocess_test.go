//go:build inprocess

package inprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// buildSeid builds the standard seid binary (no build tags — the real production
// start path the subprocess harness runs, distinct from the -tags inprocess app).
func buildSeid(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..")) // <root>/inprocess -> <root>
	bin := filepath.Join(t.TempDir(), "seid")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/seid")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build seid: %v\n%s", err, out)
	}
	return bin
}

// TestSubprocessNetwork is the Tier-2 feasibility gate: a cluster of real `seid`
// processes, provisioned by the harness and booted with `seid start`, must reach
// consensus and serve EVM — with no docker.
func TestSubprocessNetwork(t *testing.T) {
	seidBin := buildSeid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sn, err := StartSubprocess(ctx, Options{
		Validators:    3,
		ChainID:       "sei",
		TimeoutCommit: time.Second,
	}, seidBin)
	if err != nil {
		t.Fatalf("StartSubprocess: %v", err)
	}
	defer sn.Close()

	if err := sn.WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("WaitReady: %v", err)
	}
	t.Logf("subprocess cluster N=%d reached consensus + serving EVM (no docker)", sn.Len())
}

// TestSubprocessCrashRecovery exercises the control lifecycle: on a 4-node cluster
// (which keeps producing with 3/4 online), kill one node, confirm the kill is
// node-scoped and the chain still advances, then restart it and confirm it recovers
// from its on-disk WAL and rejoins — all with no docker.
func TestSubprocessCrashRecovery(t *testing.T) {
	seidBin := buildSeid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	sn, err := StartSubprocess(ctx, Options{
		Validators:    4,
		ChainID:       "sei",
		TimeoutCommit: time.Second,
	}, seidBin)
	if err != nil {
		t.Fatalf("StartSubprocess: %v", err)
	}
	defer sn.Close()
	if err := sn.WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("WaitReady: %v", err)
	}
	for i := 0; i < sn.Len(); i++ {
		if !sn.IsRunning(i) {
			t.Fatalf("node %d not running after WaitReady", i)
		}
	}

	// Kill node 3. The kill must be node-scoped: nodes 0-2 stay up.
	if err := sn.Kill(3); err != nil {
		t.Fatalf("Kill(3): %v", err)
	}
	if sn.IsRunning(3) {
		t.Fatalf("node 3 still running after Kill")
	}
	for i := 0; i < 3; i++ {
		if !sn.IsRunning(i) {
			t.Fatalf("Kill(3) also took down node %d", i)
		}
	}

	// The chain keeps producing with 3/4 validators online.
	h0, err := evmBlockHeight(ctx, sn.Node(0).EVMRPC())
	if err != nil {
		t.Fatalf("read node0 height: %v", err)
	}
	if err := waitEVMHeight(ctx, sn.Node(0).EVMRPC(), h0+3); err != nil {
		t.Fatalf("chain did not advance with node 3 down: %v", err)
	}

	// Restart node 3; it replays its on-disk WAL and rejoins.
	if err := sn.Restart(3); err != nil {
		t.Fatalf("Restart(3): %v", err)
	}
	if !sn.IsRunning(3) {
		t.Fatalf("node 3 not running after Restart")
	}
	if err := sn.Node(3).WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("node 3 did not recover after Restart: %v", err)
	}
	t.Logf("crash-recovery: killed + restarted node 3; it recovered via WAL and rejoined (no docker)")
}

// TestSubprocessSnapshot verifies the snapshot need of the operational suites: with
// SnapshotInterval set, a validator writes a cosmos state-sync snapshot into
// <home>/snapshots (the location the snapshot suite checks and a statesync joiner
// restores from) — no docker.
func TestSubprocessSnapshot(t *testing.T) {
	seidBin := buildSeid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	const interval = 10
	sn, err := StartSubprocess(ctx, Options{
		Validators:       3,
		ChainID:          "sei",
		TimeoutCommit:    time.Second,
		SnapshotInterval: interval,
	}, seidBin)
	if err != nil {
		t.Fatalf("StartSubprocess: %v", err)
	}
	defer sn.Close()
	if err := sn.WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("WaitReady: %v", err)
	}
	if err := waitEVMHeight(ctx, sn.Node(0).EVMRPC(), interval+3); err != nil {
		t.Fatalf("chain did not reach the snapshot interval: %v", err)
	}

	snapDir := filepath.Join(sn.Node(0).Home(), "snapshots")
	if err := waitSnapshot(ctx, snapDir); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("no snapshot written under %s: %v", snapDir, err)
	}
	t.Logf("snapshot: node 0 wrote a state-sync snapshot under %s (no docker)", snapDir)
}

// TestSubprocessStatesync verifies the statesync need: a late-joining non-validator
// node (sei-rpc-node) state-syncs from the running validators' snapshots to a
// height > 0 — the exact assertion of the docker statesync suite, with no docker.
func TestSubprocessStatesync(t *testing.T) {
	seidBin := buildSeid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	const interval = 10
	sn, err := StartSubprocess(ctx, Options{
		Validators:       3,
		ChainID:          "sei",
		TimeoutCommit:    time.Second,
		SnapshotInterval: interval,
	}, seidBin)
	if err != nil {
		t.Fatalf("StartSubprocess: %v", err)
	}
	defer sn.Close()
	if err := sn.WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("WaitReady: %v", err)
	}

	// A snapshot must exist before the joiner starts, or it discovers nothing and
	// loops at height 0.
	if err := waitEVMHeight(ctx, sn.Node(0).EVMRPC(), interval+3); err != nil {
		t.Fatalf("chain did not reach the snapshot interval: %v", err)
	}
	if err := waitSnapshot(ctx, filepath.Join(sn.Node(0).Home(), "snapshots")); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("no snapshot to sync from: %v", err)
	}

	rpc, err := sn.AddStatesyncNode(ctx)
	if err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("AddStatesyncNode: %v", err)
	}
	if err := waitTMHeight(ctx, rpc.TendermintRPC(), 1); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("%s did not state-sync to height > 0: %v", rpc.Name(), err)
	}
	t.Logf("statesync: %s synced from validator snapshots to height > 0 (no docker)", rpc.Name())
}

// waitTMHeight polls a node's CometBFT RPC /status until latest_block_height >=
// target or ctx fires — the statesync suite's own readiness signal. It reuses
// latestHeight's dual-shape parse (the node's /status may be enveloped or not).
func waitTMHeight(ctx context.Context, tmRPC string, target int64) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if h, ok := latestHeight(ctx, probeClient, tmRPC); ok && h >= target {
				return nil
			}
		}
	}
}

// waitSnapshot polls snapDir until it holds at least one numeric height subdir (a
// real snapshot, not just the dir the store creates at init) or ctx fires.
func waitSnapshot(ctx context.Context, snapDir string) error {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			entries, err := os.ReadDir(snapDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if _, err := strconv.Atoi(e.Name()); err == nil {
					return nil
				}
			}
		}
	}
}

// evmBlockHeight returns a node's current block height via eth_blockNumber — a
// liveness probe independent of WaitReady. It reuses the package probe client
// (bounded per-request) + getJSON, matching readiness.go.
func evmBlockHeight(ctx context.Context, url string) (int64, error) {
	const body = `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`
	raw, ok := getJSON(ctx, probeClient, http.MethodPost, url, body)
	if !ok {
		return 0, fmt.Errorf("eth_blockNumber unreachable at %s", url)
	}
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, err
	}
	if out.Result == "" {
		return 0, fmt.Errorf("empty eth_blockNumber result from %s", url)
	}
	return strconv.ParseInt(strings.TrimPrefix(out.Result, "0x"), 16, 64)
}

// waitEVMHeight polls url until its block height reaches target or ctx fires.
func waitEVMHeight(ctx context.Context, url string, target int64) error {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if h, err := evmBlockHeight(ctx, url); err == nil && h >= target {
				return nil
			}
		}
	}
}

// TestSubprocessUpgrade exercises the full upgrade path: register the upgrade
// handler on a quorum of nodes via Restart+WithEnv, pass a gov software-upgrade
// proposal, and confirm at the plan height that the upgraded nodes continue while a
// node left behind panics "UPGRADE NEEDED" — then recover it by restarting with the
// upgrade env. No docker. (The docker upgrade YAML suite can't run on the subprocess
// backend: its seid_upgrade.sh does a host-wide `pkill -f "seid start"`, which would
// kill every node — so the mechanism is proven here in Go via the harness gov API.)
func TestSubprocessUpgrade(t *testing.T) {
	seidBin := buildSeid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	const upgradeName = "v999.0.0" // valid semver; sorts after every embedded upgrade
	sn, err := StartSubprocess(ctx, Options{
		// 4 validators: 3 upgraded keep >2/3 quorum past the upgrade height while the
		// 1 left behind panics.
		Validators:      4,
		ChainID:         "sei",
		TimeoutCommit:   time.Second,
		GovVotingPeriod: 30 * time.Second,
	}, seidBin)
	if err != nil {
		t.Fatalf("StartSubprocess: %v", err)
	}
	defer sn.Close()
	if err := sn.WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("WaitReady: %v", err)
	}

	// Register the upgrade handler on nodes 0-2 via UPGRADE_VERSION_LIST (exercises
	// Restart+WithEnv). Doing the restarts before the proposal removes any race with
	// the chain reaching the plan height.
	for i := 0; i < 3; i++ {
		if err := sn.Restart(i, WithEnv("UPGRADE_VERSION_LIST="+upgradeName)); err != nil {
			t.Fatalf("Restart(%d) with upgrade env: %v", i, err)
		}
		if err := sn.Node(i).WaitReady(ctx); err != nil {
			dumpNodeLogs(t, sn)
			t.Fatalf("node %d not ready after upgrade-env restart: %v", i, err)
		}
	}

	// Schedule the upgrade a safe margin past the current height. The margin is
	// coupled to GovVotingPeriod: at ~1 block/s (TimeoutCommit) it must exceed the
	// voting period + tally so the proposal passes (plan scheduled) before the chain
	// reaches the height — but not so far the test drags. ~30s vote window, ~70 blocks.
	h, err := evmBlockHeight(ctx, sn.Node(0).EVMRPC())
	if err != nil {
		t.Fatalf("read height: %v", err)
	}
	upgradeHeight := h + 70

	id, err := sn.SubmitUpgradeProposal(ctx, upgradeName, upgradeHeight)
	if err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("SubmitUpgradeProposal: %v", err)
	}
	if err := sn.VoteYes(ctx, id); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("VoteYes: %v", err)
	}
	if err := sn.WaitProposalPassed(ctx, id); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("WaitProposalPassed: %v", err)
	}

	// At the upgrade height: nodes 0-2 apply the upgrade and keep producing (3/4);
	// node 3, with no handler, panics "UPGRADE NEEDED".
	if err := waitEVMHeight(ctx, sn.Node(0).EVMRPC(), upgradeHeight+2); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("upgraded nodes did not advance past the upgrade height: %v", err)
	}
	for i := 0; i < 3; i++ {
		if !sn.IsRunning(i) {
			dumpNodeLogs(t, sn)
			t.Fatalf("upgraded node %d stopped at the upgrade height", i)
		}
	}
	if err := waitNodeExited(ctx, sn, 3); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("baseline node 3 did not halt at the upgrade height: %v", err)
	}
	if !nodeLogContains(sn, 3, "NEEDED at height") {
		dumpNodeLogs(t, sn)
		t.Fatalf("node 3's halt was not the expected UPGRADE NEEDED panic")
	}

	// Recover node 3 by restarting it with the upgrade env — it applies the upgrade on
	// replay and rejoins (Restart+WithEnv on the panic-recovery path).
	if err := sn.Restart(3, WithEnv("UPGRADE_VERSION_LIST="+upgradeName)); err != nil {
		t.Fatalf("Restart(3) to recover: %v", err)
	}
	if err := sn.Node(3).WaitReady(ctx); err != nil {
		dumpNodeLogs(t, sn)
		t.Fatalf("node 3 did not recover after upgrade restart: %v", err)
	}
	t.Logf("upgrade: gov-scheduled software-upgrade at height %d — nodes 0-2 upgraded + continued, node 3 panicked then recovered (no docker)", upgradeHeight)
}

// waitNodeExited blocks until node i's process has exited (the reaper observed it) or
// ctx fires.
func waitNodeExited(ctx context.Context, sn *SubprocessNetwork, i int) error {
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()
	for {
		if !sn.IsRunning(i) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// nodeLogContains reports whether node i's seid.log contains substr.
func nodeLogContains(sn *SubprocessNetwork, i int, substr string) bool {
	b, err := os.ReadFile(filepath.Join(sn.Node(i).Home(), "seid.log"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), substr)
}

// dumpNodeLogs tails each node's seid.log to surface a boot failure.
func dumpNodeLogs(t *testing.T, sn *SubprocessNetwork) {
	t.Helper()
	for _, n := range sn.net.nodes {
		if app, err := os.ReadFile(filepath.Join(n.home, "config", "app.toml")); err == nil {
			t.Logf("=== %s app.toml [evm] ===", n.moniker)
			for _, line := range strings.Split(string(app), "\n") {
				if strings.Contains(line, "evm") || strings.Contains(line, "http_") || strings.Contains(line, "ws_") || strings.Contains(line, "[state") || strings.Contains(line, "ss-") || strings.Contains(line, "sc-") {
					t.Logf("  %s", line)
				}
			}
		}
		b, err := os.ReadFile(filepath.Join(n.home, "seid.log"))
		if err != nil {
			continue
		}
		s := string(b)
		if len(s) > 8000 {
			s = s[len(s)-8000:]
		}
		t.Logf("=== %s seid.log (tail) ===\n%s", n.moniker, s)
	}
}
