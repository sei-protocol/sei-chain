//go:build inprocess

// In-process arm for the standalone dApp hardhat suites. Unlike the EVM suites (compat,
// precompile, pointer), these live in a SEPARATE hardhat project — integration_test/dapp_tests,
// with its own package.json + node_modules + hardhat.config.js — so they need their own
// install/compile prep and a driver that runs from that project's dir rather than contracts/.
// This file carries the shared dApp plumbing (ensureDappProject + runHardhatDapp + dappEnv)
// and drives the NFT-marketplace + Steak suites; runner_uniswap_inprocess_test.go reuses it.
//
// Ordering: these suites store wasm (cw721_base for NFT; steak_token + steak_hub for Steak),
// so this file's name must sort AFTER runner_inprocess_test.go, whose TestInProcessSeiDBModule
// asserts a pristine max_code_id==3 baseline that a prior wasm store would break. See
// runner_precompile_inprocess_test.go for the same discipline.
package runner_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/integration_test/runner"
)

// dappDir is the standalone dApp hardhat project, relative to this package's CWD
// (integration_test/runner). Mirrors runHardhatEVM's "../../contracts".
const dappDir = "../../integration_test/dapp_tests"

// dappEnv are the vars the dApp suites read beyond InProcessEVMEnv:
//   - DAPP_TEST_ENV selects the hardhat network (seilocal) and the suites' seilocal branch.
//   - DAPP_TESTS_MNEMONIC is a FIXED throwaway 24-word test mnemonic, not a secret: the
//     deployer it derives is admin-funded via fundAddress, and the suite recovers it into
//     the shared `test` keyring (seid keys add --recover). Any valid mnemonic works; this
//     one is pinned so the derived deployer address is stable across runs.
var dappEnv = []string{
	"DAPP_TEST_ENV=seilocal",
	"DAPP_TESTS_MNEMONIC=abandon math mimic master filter design carbon crystal rookie group knife wrap absurd much snack melt grid rough chapter fever rubber humble room trophy",
}

var (
	dappProjectOnce sync.Once
	dappProjectErr  error
)

// dappReadySentinel gates the install/compile. It lives inside node_modules (so `npm ci`,
// which rm -rf's node_modules, clears it) and is written ONLY after both npm ci and compile
// succeed. Gating on it rather than on node_modules existence is load-bearing: `npm ci`
// removes then repopulates node_modules, so an interrupted install leaves a PARTIAL tree a
// bare existence check would accept — running the suites against half-installed deps (a
// phantom "Cannot find module '@uniswap/...'" red with no code change behind it).
const dappReadySentinel = "node_modules/.inprocess-ready"

// ensureDappProject installs + compiles the dApp project once per test process, mirroring
// the docker path in dapp_tests.sh. It is the host-arm equivalent: dapp_tests/node_modules
// is provisioned here; the @uniswap v2/v3 tree is a heavy (minutes) cold install, so a warm
// checkout (sentinel present) skips it. hardhat test compiles on demand, so the explicit
// compile just front-loads it.
//
// dappEnv is layered on the sub-commands because hardhat validates the whole config on every
// invocation (compile included) and every network's HD-accounts block reads
// DAPP_TESTS_MNEMONIC — an unset mnemonic fails config validation before compile even starts.
func ensureDappProject(t *testing.T) {
	t.Helper()
	dappProjectOnce.Do(func() {
		// The suites require ../../contracts/test/lib.js, whose deps resolve from
		// contracts/node_modules — which this driver does not install (the EVM arm assumes
		// it too). Fail fast with a clear message rather than an opaque subprocess
		// module-not-found if a selective run lands on a checkout without it.
		if _, err := os.Stat("../../contracts/node_modules"); err != nil {
			dappProjectErr = fmt.Errorf("contracts/node_modules missing (run `npm ci` in contracts/ first): %w", err)
			return
		}
		if _, err := os.Stat(filepath.Join(dappDir, dappReadySentinel)); err == nil {
			return
		}
		for _, args := range [][]string{{"npm", "ci"}, {"npx", "hardhat", "compile"}} {
			cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
			cmd.Dir = dappDir
			cmd.Env = append(os.Environ(), dappEnv...)
			if out, err := cmd.CombinedOutput(); err != nil {
				dappProjectErr = fmt.Errorf("dapp %v: %w\n%s", args, err, out)
				return
			}
		}
		if err := os.WriteFile(filepath.Join(dappDir, dappReadySentinel), nil, 0o600); err != nil {
			dappProjectErr = fmt.Errorf("write dapp ready sentinel: %w", err)
			return
		}
	})
	if dappProjectErr != nil {
		t.Fatalf("prepare dapp project: %v", dappProjectErr)
	}
}

// runHardhatDapp runs one dApp hardhat test file against node 0. Unlike runHardhatEVM it
// targets the standalone dApp project (dappDir) and layers dappEnv. file is project-relative
// (e.g. "steak/SteakTests.js"). The suite's bare `seid` funding/keyring calls hit node 0 via
// the shim on PATH; SEI_EVM_RPC repoints hardhat + the pointer-register EVM txs to its port.
func runHardhatDapp(t *testing.T, file string, extraEnv ...string) {
	t.Helper()
	ensureDappProject(t)
	cmd := exec.Command("npx", "hardhat", "test", "--network", "seilocal", file)
	cmd.Dir = dappDir
	cmd.Env = append(append(append(os.Environ(), runner.InProcessEVMEnv(t, sharedNet, 0)...), dappEnv...), extraEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hardhat %s failed: %v\n%s", file, err, out)
	}
}

// TestInProcessDappNFTMarketplace runs the NFT-marketplace suite: an ERC721 + a CW721-with-
// ERC721-pointer, listed/bought across associated + unassociated buyers/sellers. The pointer
// leg is why utils.js's deployCw721WithPointer had to default evmRpc to SEI_EVM_RPC.
func TestInProcessDappNFTMarketplace(t *testing.T) {
	runHardhatDapp(t, "nftMarketplace/nftMarketplaceTests.js")
}

// TestInProcessDappSteak runs the Steak liquid-staking suite (hub + cw20 token + ERC20
// pointer; bond/unbond/transfer across associated + unassociated accounts). It never calls
// harvest(), so it dodges the in-process BeginBlock reward-timing flake the distribution
// suite hits.
func TestInProcessDappSteak(t *testing.T) {
	runHardhatDapp(t, "steak/SteakTests.js")
}
