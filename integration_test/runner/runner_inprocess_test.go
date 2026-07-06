//go:build inprocess

// Package runner_test's in-process arm runs the YAML suites against an
// inprocess.Network (no docker). It is tagged `inprocess` so it never enters a
// normal runner build; the docker-backed runner_test.go (tag `yaml_integration`)
// is unaffected.
//
// Run the bank send suite in-memory:
//
//	go test -tags inprocess -run TestInProcessBankModule -v -timeout 600s ./integration_test/runner/
package runner_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

	"github.com/sei-protocol/sei-chain/inprocess"
	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// chainID is the chain-id the bank suite signs with (`--chain-id=sei` in the tx
// helpers). The in-process harness must use the same id, and the per-node
// client.toml it writes carries it so bare `seid` calls match.
const chainID = "sei"

// adminFunding mirrors the docker step2_genesis admin grant
// (1000000000000000000000usei). Large enough to cover the suite's sends + fees
// with room to spare.
func adminFunding() sdk.Coins {
	amt, ok := sdk.NewIntFromString("1000000000000000000000")
	if !ok {
		panic("bad admin funding literal")
	}
	return sdk.NewCoins(sdk.NewCoin("usei", amt))
}

// TestInProcessBankModule runs bank_module/send_funds_test.yaml end-to-end
// through the runner's in-process arm: a genesis-funded `admin` on node 0
// (the suite's signing key) drives a real bank tx + historical balance
// queries, in-memory, no docker.
func TestInProcessBankModule(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	net, err := inprocess.Start(ctx, inprocess.Options{
		// Three validators — the smallest real multi-node topology. send_funds
		// asserts only node-0 state, but a single-node run wouldn't exercise
		// cross-peer consensus. N is constrained to 1 or >=3 (never 2) by CometBFT's
		// block-sync handoff; see inprocess.Options.Validators and the package doc.
		Validators:    3,
		ChainID:       chainID,
		TimeoutCommit: time.Second,
		ExtraKeys: []inprocess.ExtraKey{
			// admin lives on node 0 only and is genesis-funded — the docker
			// localnode topology the suite signs against.
			{Name: "admin", Node: 0, Coins: adminFunding()},
		},
	})
	if err != nil {
		t.Fatalf("inprocess.Start: %v", err)
	}
	defer net.Close()

	if err := net.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	runner.RunFile(t, "../bank_module/send_funds_test.yaml",
		runner.WithInProcessNetwork(net),
	)
}
