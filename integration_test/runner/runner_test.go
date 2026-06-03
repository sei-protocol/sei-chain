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

// opts targets sei-node-0 as the default container, matching runner.py.
var opts = runner.Options{DefaultContainer: "sei-node-0"}

func TestBankModule(t *testing.T) {
	runner.RunFile(t, "../bank_module/send_funds_test.yaml", opts)
	runner.RunFile(t, "../bank_module/multi_sig_send_test.yaml", opts)
}

func TestGovModule(t *testing.T) {
	runner.RunFile(t, "../gov_module/gov_proposal_test.yaml", opts)
	runner.RunFile(t, "../gov_module/staking_proposal_test.yaml", opts)
}

func TestOracleModule(t *testing.T) {
	runner.RunFile(t, "../oracle_module/set_feeder_test.yaml", opts)
	runner.RunFile(t, "../oracle_module/verify_penalty_counts.yaml", opts)
}

func TestMintModule(t *testing.T) {
	runner.RunFile(t, "../mint_module/mint_test.yaml", opts)
}

func TestStakingModule(t *testing.T) {
	runner.RunFile(t, "../staking_module/staking_test.yaml", opts)
}

func TestDistributionModule(t *testing.T) {
	runner.RunFile(t, "../distribution_module/rewards.yaml", opts)
}

func TestTokenFactoryModule(t *testing.T) {
	runner.RunFile(t, "../tokenfactory_module/create_tokenfactory_test.yaml", opts)
}

func TestAuthzModule(t *testing.T) {
	runner.RunFile(t, "../authz_module/staking_authorization_test.yaml", opts)
	runner.RunFile(t, "../authz_module/send_authorization_test.yaml", opts)
	runner.RunFile(t, "../authz_module/generic_authorization_test.yaml", opts)
}
