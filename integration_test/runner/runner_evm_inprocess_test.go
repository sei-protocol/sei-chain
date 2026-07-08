//go:build inprocess

// In-process arm for the EVM suites driven by hardhat/npm rather than the YAML runner.
// runHardhatEVM here is shared by the whole EVM-hardhat arm (compat, precompile, pointer).
package runner_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// runHardhatEVM runs one hardhat test file against node 0 with any extra env. The suite's
// bare `seid` funding works because lib.js's isDocker() falls through to the shimmed seid
// on PATH (set, with the EVM-RPC endpoints, by InProcessEVMEnv).
func runHardhatEVM(t *testing.T, file string, extraEnv ...string) {
	t.Helper()
	cmd := exec.Command("npx", "hardhat", "test", "--network", "seilocal", "test/"+file)
	cmd.Dir = "../../contracts"
	cmd.Env = append(append(os.Environ(), runner.InProcessEVMEnv(t, sharedNet, 0)...), extraEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hardhat %s failed: %v\n%s", file, err, out)
	}
}

// TestInProcessEVMModuleCompat runs the hardhat EVM-compatibility suite against node 0.
func TestInProcessEVMModuleCompat(t *testing.T) {
	runHardhatEVM(t, "EVMCompatabilityTest.js")
}
