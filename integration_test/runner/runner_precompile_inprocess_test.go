//go:build inprocess

// In-process arm for the hardhat EVM precompile + Sei-endpoints suites (via the shared
// runHardhatEVM in runner_evm_inprocess_test.go). SeiEndpoints exercises the legacy
// sei_getLogs/sei_getBlockByNumber* methods, which the shared appOptions enables.
//
// Ordering: both suites store wasm (counter_parallel, cw20_base), so this file's name
// must sort AFTER runner_inprocess_test.go — go runs package tests in filename order,
// and TestInProcessSeiDBModule (there) asserts a pristine max_code_id==3 baseline that
// prior wasm stores would break. Beyond that wasm-code axis, these suites mutate shared
// net global state (real bank sends, delegations, gov deposits, pointer/denom registration);
// a later-sorting suite that asserts a global invariant (supply, validator power, balances)
// would see that mutation — only the wasm-code axis has a loud tripwire.
package runner_test

import "testing"

// TestInProcessEVMPrecompiles runs the bank/addr/gov/distribution/staking/wasmd precompile
// suite. The gov case only submits + deposits (no vote), so it needs no quorum — unlike
// the gov YAML suites the shared net skips.
func TestInProcessEVMPrecompiles(t *testing.T) {
	runHardhatEVM(t, "EVMPrecompileTest.js")
}

// TestInProcessSeiEndpoints runs the Sei-native JSON-RPC endpoints suite (sei_getLogs,
// sei_getBlockByNumber[ExcludeTraceFail]) against a CW20 pointer's synthetic logs.
func TestInProcessSeiEndpoints(t *testing.T) {
	runHardhatEVM(t, "SeiEndpointsTest.js")
}
