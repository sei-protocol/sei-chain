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

// TestInProcessMintModule is skipped in-process: mint_test.yaml asserts values
// fixed by the docker localnode's genesis — both the mint-release schedule and the
// total supply (the supply assertion folds in the full genesis allocation, not
// just minted tokens) — but the harness boots from ModuleBasics.DefaultGenesis
// with its own supply + mint params. Enabling it needs the localnode's genesis
// (schedule AND supply allocation) replicated in the harness genesisBuilder, not
// the mint schedule alone; the exact expected values live in mint_test.yaml.
func TestInProcessMintModule(t *testing.T) {
	t.Skip("mint needs the docker localnode genesis (mint schedule + supply allocation) replicated in-process")
}

// TestInProcessStakingModule runs the staking suite: admin delegates, redelegates,
// and unbonds across validators, each validator's operator address (valoper)
// resolved from node_admin (--bech val). It is cross-node — the YAML's `node:`
// field selects which node's keyring supplies that address — and the operator-key
// seeding is what makes it resolvable on each node.
func TestInProcessStakingModule(t *testing.T) {
	runner.RunFile(t, "../staking_module/staking_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessGovModule is skipped in-process: the param-change plumbing works with
// a shortened voting period (proposals submit, vote, and pass), but the gov YAML
// files bundle cases coupled to the docker localnode. gov_proposal_test.yaml asserts
// the localnode's fixed total supply in its rejected-burn case (== 5e21, the mint
// coupling) and tunes an expedited case to the localnode's expedited window;
// staking_proposal_test.yaml uses an expedited proposal whose 0.667 quorum the
// suite's 2-of-N vote pattern doesn't clear at N=3. Migrating needs the localnode
// genesis (supply) replicated + the expedited quorum/timing reconciled.
func TestInProcessGovModule(t *testing.T) {
	t.Skip("gov YAMLs bundle docker-localnode-coupled cases (fixed supply burn + expedited quorum/timing)")
}

// TestInProcessStartupModule is skipped in-process: startup_test.yaml asserts the
// docker localnode's fixed 4-validator topology (`tendermint-validator-set` count
// == 4), but the shared harness network runs N=3.
func TestInProcessStartupModule(t *testing.T) {
	t.Skip("startup asserts the docker localnode's fixed 4-validator topology; harness runs N=3")
}

// TestInProcessTokenfactoryModule creates/mints/burns/changes-admin on a
// tokenfactory denom as admin (a fresh new_admin_addr key, so no keyring collision).
func TestInProcessTokenfactoryModule(t *testing.T) {
	runner.RunFile(t, "../tokenfactory_module/create_tokenfactory_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessOracleModule sets a feeder for node_admin's valoper and verifies
// oracle penalty counts.
func TestInProcessOracleModule(t *testing.T) {
	runner.RunFile(t, "../oracle_module/set_feeder_test.yaml", runner.WithInProcessNetwork(sharedNet))
	runner.RunFile(t, "../oracle_module/verify_penalty_counts.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessSeiDBModule is skipped in-process: state_store_test.yaml asserts a
// docker fixture — 300 wasm contracts stored across block heights tracked in
// integration_test/contracts/wasm_*_block_height.txt — which the shared harness
// network doesn't build, so every historical list-code query returns empty.
func TestInProcessSeiDBModule(t *testing.T) {
	t.Skip("seidb state_store asserts a docker fixture (300 wasm contracts at tracked heights)")
}

// TestInProcessBankMultiSig builds a 2-of-3 multisig (fresh wallet1/2/3 keys) and
// sends through it.
func TestInProcessBankMultiSig(t *testing.T) {
	runner.RunFile(t, "../bank_module/multi_sig_send_test.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessBankSimulation dry-run-simulates a bank send (fresh simulation-test
// key).
func TestInProcessBankSimulation(t *testing.T) {
	runner.RunFile(t, "../bank_module/simulation_tx.yaml", runner.WithInProcessNetwork(sharedNet))
}

// TestInProcessWasmModule is skipped in-process: the timelocked-token suites execute
// against a docker-fixture contract — a pre-deployed gringotts instance whose address
// (and the admin1 signer) come from integration_test/contracts/gringotts-contract-addr.txt,
// which the shared harness network doesn't build. Migrating it needs that contract
// deployed + its address seeded in-process.
func TestInProcessWasmModule(t *testing.T) {
	t.Skip("wasm timelocked suites assert a docker fixture (pre-deployed gringotts contract + admin1)")
}

// TestInProcessFlatKVEvmModule is skipped in-process: flatkv_evm_test.yaml asserts a
// docker fixture — a pre-deployed EVM contract with recorded balances/heights read
// from integration_test/contracts/flatkv_evm_*.txt — not built by the shared network.
func TestInProcessFlatKVEvmModule(t *testing.T) {
	t.Skip("seidb flatkv_evm asserts a docker fixture (pre-deployed EVM contract + recorded balances/heights)")
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
