//go:build inprocess

// Package runner_giga_test runs the hardhat EVMGigaTest suite in-process against a
// network whose Giga executor + OCC are EXPLICITLY PINNED on via
// Options.AppConfigOverride, rather than resting on gigaconfig.DefaultConfig being
// giga-on. The pin is load-bearing: a one-line flip of DefaultConfig.Enabled would
// otherwise silently downgrade this suite's coverage to the V2 executor, and the
// post-boot guard (below) turns that downgrade into a loud failure.
//
// It is a dedicated package/process because the harness allows one network per
// process (app.New's EVM worker pool / metrics printer / Prometheus registries are
// process-global singletons) and this network's giga pin is distinct from the shared
// N=3 net's defaults. N=1 suffices — EVMGigaTest drives a single RPC endpoint.
//
//	go test -tags inprocess -v -timeout 900s ./integration_test/runner_giga/
package runner_giga_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/executor/config"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/inprocess"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// chainID matches the id the hardhat suite's bare `seid` funding signs with; the
// harness writes it into each node's client.toml.
const chainID = "sei"

// adminFunding mirrors the docker step2_genesis admin grant — the balance `admin`
// draws on to fund each hardhat signer (setupSigners → fundAddress → `seid tx evm
// send --from admin`).
func adminFunding() sdk.Coins {
	amt, ok := sdk.NewIntFromString("1000000000000000000000")
	if !ok {
		panic("bad admin funding literal")
	}
	return sdk.NewCoins(sdk.NewCoin("usei", amt))
}

// gigaNet is the pinned-giga network EVMGigaTest runs against; TestMain owns its
// lifecycle and is non-nil for the duration of m.Run().
var gigaNet *inprocess.Network

func TestMain(m *testing.M) { os.Exit(runSuites(m)) }

// runSuites brings up the pinned-giga network, gates it on the post-boot giga
// assertion, then runs the suite. Split from TestMain so the deferred Close/cancel
// run before os.Exit (which skips defers).
func runSuites(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	admin := adminFunding()
	net, err := inprocess.Start(ctx, inprocess.Options{
		Validators:    1, // solo proposer; EVMGigaTest hits one RPC
		ChainID:       chainID,
		TimeoutCommit: time.Second,
		// The pin: consts, never string literals — a literal typo would no-op the
		// override and fall back to the default, defeating the whole point.
		AppConfigOverride: map[string]any{
			gigaconfig.FlagEnabled:    true,
			gigaconfig.FlagOCCEnabled: true,
		},
		ExtraKeys: []inprocess.ExtraKey{
			{Name: "admin", Node: 0, Coins: admin},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "inprocess.Start: %v\n", err)
		return 1
	}
	defer net.Close()

	if err := net.WaitReady(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "WaitReady: %v\n", err)
		return 1
	}

	// Post-boot downgrade guard, gating the WHOLE hardhat run (not a subtest a `-run`
	// filter could skip): assert the running node actually resolved onto the giga
	// path. This is what makes the pin meaningful — it catches every downgrade the
	// pin defends against (a flipped DefaultConfig.Enabled, a renamed flag const, an
	// OCC-off regression), because the values are read back off the running app, not
	// the requested config. It proves the giga path is SELECTED, not that evmone loaded
	// (evmone is dlopen'd best-effort with no execution read-site, so its absence doesn't
	// make giga vacuous). This is the single-mode giga analogue of docker's
	// giga-integration-test; the mixed giga/V2 consensus-divergence guard stays docker-only
	// (an in-process mesh can't differ per-node — see the GIGA-Mixed Tier-2 LLD).
	if node := net.Node(0); !node.GigaExecutorEnabled() || !node.GigaOCCEnabled() {
		fmt.Fprintf(os.Stderr,
			"giga pin not honored: node0 GigaExecutorEnabled=%t GigaOCCEnabled=%t (expected both true) — did DefaultConfig or a flag const change?\n",
			node.GigaExecutorEnabled(), node.GigaOCCEnabled())
		return 1
	}

	gigaNet = net
	return m.Run()
}

// TestGigaEVM runs the hardhat EVMGigaTest suite against the pinned-giga node 0. The
// suite's bare `seid` funding works because lib.js falls through to the shimmed seid
// on PATH (set, with the EVM-RPC endpoints, by InProcessEVMEnv).
func TestGigaEVM(t *testing.T) {
	cmd := exec.Command("npx", "hardhat", "test", "--network", "seilocal", "test/EVMGigaTest.js")
	cmd.Dir = "../../contracts"
	cmd.Env = append(os.Environ(), runner.InProcessEVMEnv(t, gigaNet, 0)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hardhat EVMGigaTest.js failed: %v\n%s", err, out)
	}
}
