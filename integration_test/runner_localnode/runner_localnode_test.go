//go:build inprocess

// Package runner_localnode_test runs the docker-localnode-parity suites (mint,
// startup, gov param-change + expedited) against a dedicated N=4 in-process network
// whose genesis matches step2_genesis.sh — 5e21 usei supply, the ~3-day mint window,
// 4 validators, and the docker gov params. It is a separate package/process from the
// N=3 shared net (runner_inprocess_test.go) because the harness allows one network
// per process and this genesis differs.
//
// Parity is scoped to supply/mint/gov: oracle + slashing params are left at module
// defaults, which already disable oracle jailing (feeder retired → min-valid-per-window
// 0) and put downtime jailing ~28h out — so the 4-validator set never churns during a
// run. A suite asserting oracle/slashing behavior would need those params replicated.
//
//	go test -tags inprocess -v -timeout 900s ./integration_test/runner_localnode/
package runner_localnode_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/inprocess"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

const chainID = "sei"

// mustInt parses an integer literal, panicking on a bad constant (a programming error).
func mustInt(literal string) sdk.Int {
	amt, ok := sdk.NewIntFromString(literal)
	if !ok {
		panic("bad int literal: " + literal)
	}
	return amt
}

// usei is a usei-coin amount from an integer literal.
func usei(literal string) sdk.Coins { return sdk.NewCoins(sdk.NewCoin("usei", mustInt(literal))) }

// sharedNet is the one N=4 network the localnode-parity suites run against; TestMain
// owns its lifecycle. Suites run sequentially (no t.Parallel) — one chain, shared state.
var sharedNet *inprocess.Network

func TestMain(m *testing.M) { os.Exit(runSuites(m)) }

func runSuites(m *testing.M) int {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	supply := mustInt("5000000000000000000000") // 5e21: 4×1e21 validators + 1e21 admin
	net, err := inprocess.Start(ctx, inprocess.Options{
		Validators:        4, // startup asserts valset==4; gov needs 4 equal-power validators
		ChainID:           chainID,
		TimeoutCommit:     time.Second,
		GenesisUseiSupply: &supply, // → TOTAL_SUPPLY 5000000000333333333333 after one mint release
		MintTokenReleaseSchedule: []inprocess.MintRelease{
			{StartDaysFromGenesis: 0, EndDaysFromGenesis: 3, Amount: sdk.NewInt(999999999999)},
		},
		GovParams: inprocess.DockerLocalnodeGovParams(),
		ExtraKeys: []inprocess.ExtraKey{
			{Name: "admin", Node: 0, Coins: usei("1000000000000000000000")}, // gov deposits + fees
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
	sharedNet = net
	return m.Run()
}

// TestLocalnodeMint asserts the exact post-first-release supply. Exactly one release
// fires per UTC day (the minter dedups same-day), and mint_test's own sleep crosses the
// first epoch (~65s), so the read sees exactly one — stable regardless of suite order.
// skipIfNearUTCMidnight avoids a run straddling midnight, which would fire a second.
func TestLocalnodeMint(t *testing.T) {
	skipIfNearUTCMidnight(t)
	runner.RunFile(t, "../mint_module/mint_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestLocalnodeStartup asserts the 4-validator topology; time-independent.
func TestLocalnodeStartup(t *testing.T) {
	runner.RunFile(t, "../startup/startup_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestLocalnodeGov runs the param-change + expedited cases. Votes span sei-node-0..3 —
// 2/4 clears quorum 0.5, 4/4 clears expedited 0.9.
func TestLocalnodeGov(t *testing.T) {
	runner.RunFile(t, "../gov_module/gov_param_and_expedited_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestLocalnodeGovRejectedBurn migrates the docker rejected-burn case. The docker YAML
// asserts an absolute supply the long-lived net has moved past; the localnode variant
// asserts the relative supply delta (burned deposit) instead, so mint timing can't
// perturb it. skipIfNearUTCMidnight covers the one residual coupling — a next-day mint
// release firing inside the burn window when the net's lifetime crosses UTC midnight.
func TestLocalnodeGovRejectedBurn(t *testing.T) {
	skipIfNearUTCMidnight(t)
	runner.RunFile(t, "../gov_module/gov_rejected_burn_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// skipIfNearUTCMidnight skips within slack of UTC midnight: the mint schedule is
// date-keyed, so a network lifetime crossing midnight can observe a second daily
// release, breaking the exact TOTAL_SUPPLY/REMAINING assertions. A once-a-day skip,
// not a flake.
func skipIfNearUTCMidnight(t *testing.T) {
	const slack = 5 * time.Minute
	now := time.Now().UTC()
	if nextMidnight := now.Truncate(24 * time.Hour).Add(24 * time.Hour); nextMidnight.Sub(now) < slack {
		t.Skipf("within %s of UTC midnight; the date-keyed mint schedule would flake", slack)
	}
}
