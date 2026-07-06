//go:build inprocess

// Package runner_subprocess's Tier-2 arm runs the YAML suites against a shared
// cluster of real `seid` PROCESSES (inprocess.SubprocessNetwork) — no docker, no
// in-goroutine node. It proves the same suites the in-process arm runs also pass
// against real seid processes booted with `seid start`, and it is the future home
// for the operational suites (upgrade / crash-recovery / statesync) that need the
// real process boundary the in-process arm cannot provide.
//
// One subprocess cluster is brought up in TestMain and reused across suites,
// mirroring the docker job model and the in-process arm; suites run sequentially
// against it.
//
//	go test -tags inprocess -v -timeout 900s ./integration_test/runner_subprocess/
package runner_subprocess

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/inprocess"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// chainID is the chain-id the suites sign with (`--chain-id=sei`); the harness and
// the per-node client.toml it writes carry the same id.
const chainID = "sei"

// adminFunding mirrors the docker step2_genesis admin grant
// (1000000000000000000000usei), matching the in-process arm so the shared
// send-suite YAMLs behave identically.
func adminFunding() sdk.Coins {
	amt, ok := sdk.NewIntFromString("1000000000000000000000")
	if !ok {
		panic("bad admin funding literal")
	}
	return sdk.NewCoins(sdk.NewCoin("usei", amt))
}

// sharedNet is the one subprocess cluster every suite runs against. TestMain owns
// its lifecycle; it is non-nil for the duration of m.Run(). Suites run
// sequentially (the per-node keyring the suites mutate is not concurrent-safe), so
// no suite may call t.Parallel().
var sharedNet *inprocess.SubprocessNetwork

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run brings up the shared subprocess cluster, runs the suites, and tears it down.
// It is split from TestMain so the deferred Close/cleanup run before os.Exit (which
// skips defers).
func run(m *testing.M) int {
	// go test's own -timeout is the authoritative run bound; this generous ctx is
	// the backstop that parents the node processes (StartSubprocess binds each to
	// it), so it must outlive m.Run().
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	seidBin, binDir, err := buildSeid()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build seid: %v\n", err)
		return 1
	}
	defer os.RemoveAll(binDir)

	sn, err := inprocess.StartSubprocess(ctx, inprocess.Options{
		// Three validators — the smallest real multi-node topology (see
		// inprocess.Options.Validators). admin is genesis-funded on node 0 (the
		// suites' signing key), exactly as the in-process arm funds it.
		Validators:    3,
		ChainID:       chainID,
		TimeoutCommit: time.Second,
		// Take a state-sync snapshot every 10 blocks so the snapshot suite finds one
		// and the statesync joiner has something to restore from.
		SnapshotInterval: 10,
		ExtraKeys: []inprocess.ExtraKey{
			{Name: "admin", Node: 0, Coins: adminFunding()},
		},
	}, seidBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "StartSubprocess: %v\n", err)
		return 1
	}
	defer sn.Close()

	if err := sn.WaitReady(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "WaitReady: %v\n%s", err, sn.LogTails())
		return 1
	}

	// Add the late-joining sei-rpc-node once a snapshot exists (AddStatesyncNode
	// waits for one), then wait for it to state-sync and start advancing — so the
	// statesync suite finds it at height > 0.
	rpc, err := sn.AddStatesyncNode(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "AddStatesyncNode: %v\n%s", err, sn.LogTails())
		return 1
	}
	if err := rpc.WaitReady(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "statesync node WaitReady: %v\n%s", err, sn.LogTails())
		return 1
	}
	sharedNet = sn
	return m.Run()
}

// buildSeid builds the production seid binary (no build tags — the real `seid
// start` path the subprocess nodes run) into a temp dir, returning the binary path
// and its dir for cleanup.
func buildSeid() (bin, dir string, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	// go test runs with CWD = the package dir; climb to the module root so `go build
	// ./cmd/seid` resolves.
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	dir, err = os.MkdirTemp("", "sei-subprocess-node-")
	if err != nil {
		return "", "", err
	}
	bin = filepath.Join(dir, "seid")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/seid")
	cmd.Dir = root
	if out, berr := cmd.CombinedOutput(); berr != nil {
		_ = os.RemoveAll(dir)
		return "", "", fmt.Errorf("go build seid: %w\n%s", berr, out)
	}
	return bin, dir, nil
}

// TestSubprocessBankModule runs the bank send suite against the shared subprocess
// cluster: a genesis-funded `admin` on node 0 drives a real bank tx + historical
// balance queries against a real `seid` process, no docker.
func TestSubprocessBankModule(t *testing.T) {
	runner.RunFile(t, "../bank_module/send_funds_test.yaml", runner.WithSubprocessNetwork(sharedNet))
}

// TestSubprocessSnapshotModule runs the docker snapshot suite unchanged against the
// subprocess cluster: node 0 (SnapshotInterval set) has written a state-sync
// snapshot, which the suite finds via SEI_SNAPSHOT_DIR — no docker.
func TestSubprocessSnapshotModule(t *testing.T) {
	runner.RunFile(t, "../chain_operation/snapshot_operation.yaml", runner.WithSubprocessNetwork(sharedNet))
}

// TestSubprocessStatesyncModule runs the docker statesync suite unchanged against
// the subprocess cluster: the late-joining sei-rpc-node (added in TestMain) has
// state-synced from the validators' snapshots to height > 0 — no docker.
func TestSubprocessStatesyncModule(t *testing.T) {
	runner.RunFile(t, "../chain_operation/statesync_operation.yaml", runner.WithSubprocessNetwork(sharedNet))
}
