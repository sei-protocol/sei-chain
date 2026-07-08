//go:build inprocess

package runner_localnode_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// TestInProcessDisableWasm runs the hardhat Disable-WASM suite against the N=4
// localnode gov net. The suite toggles the wasm upload/instantiate access params
// off ("Nobody") and back on ("Everybody") through expedited gov proposals, then
// asserts store/instantiate is rejected while disabled but existing CW20 pointers
// still query + execute. There is no no-wasm binary: the app is wasm-enabled and the
// disable is purely a governed param flip.
//
// The proposals clear because the net carries DockerLocalnodeGovParams and
// InProcessGovNodesEnv hands the suite every validator's home, so its passProposal
// casts node_admin's yes on all four — 4/4 meets the 0.9 expedited quorum +
// threshold. The wasm params are not supply-coupled, so the case is stable on the
// long-lived shared net.
func TestInProcessDisableWasm(t *testing.T) {
	cmd := exec.Command("npx", "hardhat", "test", "--network", "seilocal", "test/DisableWasmTest.js")
	cmd.Dir = "../../contracts"
	env := append(os.Environ(), runner.InProcessEVMEnv(t, sharedNet, 0)...)
	env = append(env, runner.InProcessGovNodesEnv(sharedNet)...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hardhat DisableWasmTest.js failed: %v\n%s", err, out)
	}
}
