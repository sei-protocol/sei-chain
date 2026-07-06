//go:build inprocess

// Package runner_test's in-process arm runs the YAML suites against a single
// shared inprocess.Network (no docker), started once in TestMain and reused by
// every suite. This mirrors the docker job model — one cluster, suites run
// sequentially against it — and amortizes the ~8s bring-up + one seid build
// across all suites.
//
// Suites share one chain + keyring, so a shared signer (admin, node_admin)
// accumulates balances, sequence numbers, and keyring entries across suites in run
// order. Assert deltas or query pinned heights — never an absolute latest-state
// value for a shared signer (the docker job shares state the same way).
//
// A shared network is required, not merely efficient: the harness allows only one
// in-process network per process (the EVM worker pool / metrics printer /
// Prometheus registries are process-global singletons), so a per-suite Start
// would fail the second time.
//
// It is tagged `inprocess` so it never enters a normal runner build; the
// docker-backed runner_test.go (tag `yaml_integration`) is unaffected.
//
//	go test -tags inprocess -v -timeout 900s ./integration_test/runner/
package runner_test

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

// chainID is the chain-id the suites sign with (`--chain-id=sei`). The harness
// uses the same id, and the per-node client.toml it writes carries it so bare
// `seid` calls match.
const chainID = "sei"

// adminFunding mirrors the docker step2_genesis admin grant
// (1000000000000000000000usei) — large enough to cover every suite's sends + fees
// across the shared network's lifetime.
func adminFunding() sdk.Coins {
	amt, ok := sdk.NewIntFromString("1000000000000000000000")
	if !ok {
		panic("bad admin funding literal")
	}
	return sdk.NewCoins(sdk.NewCoin("usei", amt))
}

// sharedNet is the one in-process network every suite runs against. TestMain owns
// its lifecycle; it is non-nil for the duration of m.Run(). Suites must run
// sequentially — the Network (and the per-node keyring the suites mutate) is not
// goroutine-safe, so no TestInProcess* suite may call t.Parallel().
var sharedNet *inprocess.Network

func TestMain(m *testing.M) {
	os.Exit(runSuites(m))
}

// runSuites brings up the shared network, runs the suites, and tears it down. It
// is split from TestMain so the deferred Close/cancel run before os.Exit (which
// skips defers).
func runSuites(m *testing.M) int {
	// go test's own -timeout is the authoritative run bound (a hang panics there,
	// skipping defers); this generous ctx is the backstop for -timeout 0 and the
	// parent for the nodes, so it must outlive m.Run().
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	net, err := inprocess.Start(ctx, inprocess.Options{
		// Three validators — the smallest real multi-node topology (N is 1 or >=3;
		// see inprocess.Options.Validators). admin is genesis-funded on node 0 (the
		// suites' signing key); each node's validator operator is seeded as
		// node_admin by the harness, which the distribution/staking suites resolve.
		Validators:    3,
		ChainID:       chainID,
		TimeoutCommit: time.Second,
		ExtraKeys: []inprocess.ExtraKey{
			{Name: "admin", Node: 0, Coins: adminFunding()},
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

// TestInProcessBankModule runs the bank send suite against the shared network: a
// genesis-funded `admin` on node 0 drives a real bank tx + historical balance
// queries, in-memory, no docker.
func TestInProcessBankModule(t *testing.T) {
	runner.RunFile(t, "../bank_module/send_funds_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessDistributionModule funds the community pool (admin) and withdraws
// validator rewards (node_admin). The latter exercises the operator-key seeding:
// node_admin self-bonds at genesis, so it accrues rewards and resolves a valoper,
// exactly as the docker localnode provides.
func TestInProcessDistributionModule(t *testing.T) {
	runner.RunFile(t, "../distribution_module/community_pool.yaml", runner.WithInProcessNetwork(sharedNet))
	runner.RunFile(t, "../distribution_module/rewards.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessAuthzModule is skipped in-process: the staking/generic YAMLs
// re-`keys add grantee` (a name the send suite already created) and feed
// `printf "<pass>\ny\n"` to answer docker's passphrase-then-overwrite prompts. The
// harness's `test` keyring takes no passphrase, so the first line is consumed as
// the overwrite answer and the add aborts. Enabling authz needs keyring-backend
// parity or per-suite key isolation.
func TestInProcessAuthzModule(t *testing.T) {
	t.Skip("authz needs keyring-backend parity or per-suite key isolation")
}
