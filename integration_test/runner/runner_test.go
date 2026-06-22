//go:build yaml_integration

// Package runner_test wires the existing YAML test files into standard `go test`.
//
// Requires a running Docker cluster (`make docker-cluster-start`).
//
// Run a single module:
//
//	go test -tags yaml_integration -v ./integration_test/runner/... -run TestBankModule
//
// Run all modules:
//
//	go test -tags yaml_integration -v ./integration_test/runner/...
//
// Each YAML test case becomes a named subtest, so -run accepts the full path:
//
//	-run TestBankModule/Test_sending_funds
package runner_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

func TestStartup(t *testing.T) {
	runner.RunFile(t, "../startup/startup_test.yaml")
}

// Tests are declared in the order the CI matrix ran them as Python scripts
// (go test executes tests in declaration order): staking, then bank, then mint.

func TestStakingModule(t *testing.T) {
	runner.RunFile(t, "../staking_module/staking_test.yaml")
}

func TestBankModule(t *testing.T) {
	runner.RunFile(t, "../bank_module/send_funds_test.yaml")
	runner.RunFile(t, "../bank_module/multi_sig_send_test.yaml")
	runner.RunFile(t, "../bank_module/simulation_tx.yaml")
}

// TestAutobahnBankModule is the Autobahn-matrix bank slice. It runs only
// send_funds_test.yaml because the multi_sig and simulation cases broadcast
// with -b block, which Autobahn's KV indexer doesn't support (BroadcastTxCommit
// hangs to its timeout).
func TestAutobahnBankModule(t *testing.T) {
	runner.RunFile(t, "../bank_module/send_funds_test.yaml")
}

func TestMintModule(t *testing.T) {
	runner.RunFile(t, "../mint_module/mint_test.yaml")
}

func TestGovModule(t *testing.T) {
	runner.RunFile(t, "../gov_module/gov_proposal_test.yaml")
	runner.RunFile(t, "../gov_module/staking_proposal_test.yaml")
}

func TestOracleModule(t *testing.T) {
	runner.RunFile(t, "../oracle_module/verify_penalty_counts.yaml")
	runner.RunFile(t, "../oracle_module/set_feeder_test.yaml")
}

func TestDistributionModule(t *testing.T) {
	runner.RunFile(t, "../distribution_module/community_pool.yaml")
	runner.RunFile(t, "../distribution_module/rewards.yaml")
}

func TestTokenFactoryModule(t *testing.T) {
	runner.RunFile(t, "../tokenfactory_module/create_tokenfactory_test.yaml")
}

func TestAuthzModule(t *testing.T) {
	runner.RunFile(t, "../authz_module/send_authorization_test.yaml")
	runner.RunFile(t, "../authz_module/staking_authorization_test.yaml")
	runner.RunFile(t, "../authz_module/generic_authorization_test.yaml")
}

// TestWasmModuleCore runs delegation, admin, and withdraw tests after a fresh contract deploy.
// In CI a second deploy follows before TestWasmModuleEmergencyWithdraw, because the withdraw
// test mutates contract state that the emergency-withdraw test depends on.
func TestWasmModuleCore(t *testing.T) {
	runner.RunFile(t, "../wasm_module/timelocked_token_delegation_test.yaml")
	runner.RunFile(t, "../wasm_module/timelocked_token_admin_test.yaml")
	runner.RunFile(t, "../wasm_module/timelocked_token_withdraw_test.yaml")
}

// TestWasmModuleEmergencyWithdraw requires a freshly deployed timelocked token contract.
// In CI it runs after a second invocation of deploy_timelocked_token_contract.sh.
func TestWasmModuleEmergencyWithdraw(t *testing.T) {
	runner.RunFile(t, "../wasm_module/timelocked_token_emergency_withdraw_test.yaml")
}

// TestSeiDBFlatKV runs the FlatKV EVM integration scenarios.
// In CI the cluster boots with GIGA_STORAGE=true and a fixture is deployed first.
func TestSeiDBFlatKV(t *testing.T) {
	runner.RunFile(t, "../seidb/flatkv_evm_test.yaml")
}

// TestSeiDBStateStore runs state-store iteration tests.
// In CI wasm contracts and tokenfactory denoms are deployed as a prerequisite.
func TestSeiDBStateStore(t *testing.T) {
	runner.RunFile(t, "../seidb/state_store_test.yaml")
}

func TestChainOperation(t *testing.T) {
	runner.RunFile(t, "../chain_operation/snapshot_operation.yaml")
}

// TestStateSync requires the sei-rpc-node container, which is only present in
// the CI RPC-node cluster (make run-rpc-node-integration-ci).
func TestStateSync(t *testing.T) {
	runner.RunFile(t, "../chain_operation/statesync_operation.yaml")
}

// TestUpgradeMajor tests a major release upgrade: early-upgrade panic, target-height panic,
// rolling node upgrades, and full recovery across all four nodes.
func TestUpgradeMajor(t *testing.T) {
	runner.RunFile(t, "../upgrade_module/major_upgrade_test.yaml")
}

// TestUpgradeMinor tests a minor release upgrade: early upgrades continue without panic,
// non-upgraded nodes panic at target height, and rolling upgrades complete successfully.
func TestUpgradeMinor(t *testing.T) {
	runner.RunFile(t, "../upgrade_module/minor_upgrade_test.yaml")
}
