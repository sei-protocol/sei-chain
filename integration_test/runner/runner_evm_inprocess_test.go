//go:build inprocess

// In-process arm for the EVM suites driven by hardhat/npm rather than the YAML
// runner. See runner.InProcessEVMEnv.
package runner_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// TestInProcessEVMModuleCompat runs the hardhat EVM-compatibility suite against node 0
// via InProcessEVMEnv. The suite's bare `seid` funding works because lib.js's
// isDocker() falls through to the shimmed seid on PATH.
func TestInProcessEVMModuleCompat(t *testing.T) {
	cmd := exec.Command("npx", "hardhat", "test", "--network", "seilocal", "test/EVMCompatabilityTest.js")
	cmd.Dir = "../../contracts"
	cmd.Env = append(os.Environ(), runner.InProcessEVMEnv(t, sharedNet, 0)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hardhat EVM compat suite failed: %v\n%s", err, out)
	}
}
