//go:build inprocess

// In-process arm for the Uniswap V3 dApp suite, riding the shared dApp plumbing
// (ensureDappProject + runHardhatDapp + dappEnv) in runner_steaknft_inprocess_test.go.
// The heaviest EVM suite: before() deploys ~6 large contracts (WETH9, V3 factory/manager/
// router, NFT descriptor, mocks) plus a tokenfactory-pointer and a CW20-pointer, opens 3
// pools, and supplies liquidity, then runs 8 sequential swap/pool cases. Its whole budget
// rides go test's -timeout (mocha's own timeout is effectively unbounded).
//
// Ordering: the CW20-pointer leg stores wasm (cw20_base), so this file's name must sort
// AFTER runner_inprocess_test.go's max_code_id==3 baseline — same discipline as
// runner_steaknft_inprocess_test.go (install is sync.Once-guarded, so order-independent).
package runner_test

import "testing"

// TestInProcessDappUniswap runs the Uniswap V3 suite: swaps + pool ops across ERC20,
// tokenfactory-pointer, and CW20-pointer tokens, for associated + unassociated accounts.
func TestInProcessDappUniswap(t *testing.T) {
	runHardhatDapp(t, "uniswap/uniswapTest.js")
}
