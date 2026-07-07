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
			// seidb_creator is a pristine wasm/tokenfactory creator for the SeiDB
			// suite: its historical counts assume a creator with no prior codes/denoms,
			// but `admin` is polluted (the tokenfactory suite creates a denom as admin).
			{Name: "seidb_creator", Node: 0, Coins: adminFunding()},
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

// TestInProcessSeiDBModule runs the state_store historical-query suite (wasm
// list-code + tokenfactory denom counts at recorded heights). The EVM module's
// InitGenesis stores 3 CW-pointer codes (erc20/721/1155) as ids 1-3 on every fresh
// chain — same as docker — so the fixtures' 30 codes are the deterministic ids 4-33
// the suite asserts; deploy_wasm_contracts.sh guards that baseline (fails loudly if
// a pointer is ever added/removed, or another suite stored wasm first). Both run as
// seidb_creator, not admin (a pristine creator; see runSuites).
// FIXTURE_SETTLE_SECONDS=0 drops the docker KV-indexer sleeps — the seq-poll gates
// commits, and --height queries don't care about settle time.
func TestInProcessSeiDBModule(t *testing.T) {
	runner.RunFile(t, "../seidb/state_store_test.yaml",
		runner.WithInProcessNetwork(sharedNet),
		runner.WithSetupScripts(
			"integration_test/contracts/deploy_wasm_contracts.sh",
			"integration_test/contracts/create_tokenfactory_denoms.sh",
		),
		runner.WithSetupEnv(map[string]string{
			"SEIDBIN":                "seid",
			"FIXTURE_SIGNER":         "seidb_creator",
			"FIXTURE_SETTLE_SECONDS": "0",
		}))
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

// timelockedFixture is the gringotts bring-up docker runs before each wasm group
// (deploy goblin + gringotts, seed admin1-4/op/etc, record the addresses).
// WithSetupScripts runs it in-process through the seid shim; WithIsolatedKeyring
// gives each group its own overlay so the fixture's repeated `keys add admin1`
// doesn't hit the override prompt on the second deploy.
const timelockedFixture = "integration_test/contracts/deploy_timelocked_token_contract.sh"

// timelockedSetupEnv points the fixture at the shimmed seid (SEIDBIN) and the
// genesis-funded signer (FIXTURE_SIGNER); node targeting is added by the arm.
var timelockedSetupEnv = map[string]string{"SEIDBIN": "seid", "FIXTURE_SIGNER": "admin"}

// TestInProcessWasmModuleCore runs delegation → admin → withdraw against one fresh
// gringotts deploy, mirroring docker's TestWasmModuleCore. The three share the
// suite's single deploy + keyring; order matters (withdraw depends on prior state).
func TestInProcessWasmModuleCore(t *testing.T) {
	s := runner.NewInProcessSuite(t, sharedNet,
		runner.WithSetupScripts(timelockedFixture),
		runner.WithSetupEnv(timelockedSetupEnv),
		runner.WithIsolatedKeyring())
	s.RunFile("../wasm_module/timelocked_token_delegation_test.yaml")
	s.RunFile("../wasm_module/timelocked_token_admin_test.yaml")
	s.RunFile("../wasm_module/timelocked_token_withdraw_test.yaml")
}

// TestInProcessWasmModuleEmergencyWithdraw needs a pristine gringotts (Core's flows
// mutate the contract), so it deploys its own fixture — the second deploy docker
// runs before TestWasmModuleEmergencyWithdraw.
func TestInProcessWasmModuleEmergencyWithdraw(t *testing.T) {
	s := runner.NewInProcessSuite(t, sharedNet,
		runner.WithSetupScripts(timelockedFixture),
		runner.WithSetupEnv(timelockedSetupEnv),
		runner.WithIsolatedKeyring())
	s.RunFile("../wasm_module/timelocked_token_emergency_withdraw_test.yaml")
}

// TestInProcessFlatKVEvmModule deploys the flatkv EVM fixture (a storage contract,
// an EVM transfer, and admin associate-address — all via cast + seid) then runs the
// historical --block balance/storage/code queries against it. It runs on the shared
// network: SeiDB SC+SS (see appoptions.go) retain the recorded heights, and the
// fixture adds no keyring names, so no isolation is needed. BULK_STORAGE_KEYS=0
// skips the ~80-block bulk deploy the YAML never asserts. Needs cast (Foundry) on PATH.
func TestInProcessFlatKVEvmModule(t *testing.T) {
	runner.RunFile(t, "../seidb/flatkv_evm_test.yaml",
		runner.WithInProcessNetwork(sharedNet),
		runner.WithSetupScripts("integration_test/contracts/deploy_flatkv_evm_fixture.sh"),
		runner.WithSetupEnv(map[string]string{
			"FLATKV_EVM_FIXTURE_KEYRING_BACKEND": "test",
			"FLATKV_EVM_BULK_STORAGE_KEYS":       "0",
		}))
}

// TestInProcessAuthzModule runs the three authz suites, each with an isolated
// keyring (WithIsolatedKeyring). Each suite `keys add grantee` under docker's
// `printf "<pass>\ny\n"`; on the shared `test` keyring the second suite's re-add of
// an existing `grantee` would hit the override prompt and abort. A per-suite keyring
// overlay makes `grantee` fresh each time, so the add succeeds and the piped input
// is harmlessly ignored — no YAML edit, no keyring-backend change.
func TestInProcessAuthzModule(t *testing.T) {
	for _, f := range []string{
		"../authz_module/send_authorization_test.yaml",
		"../authz_module/staking_authorization_test.yaml",
		"../authz_module/generic_authorization_test.yaml",
	} {
		runner.RunFile(t, f, runner.WithInProcessNetwork(sharedNet), runner.WithIsolatedKeyring())
	}
}
