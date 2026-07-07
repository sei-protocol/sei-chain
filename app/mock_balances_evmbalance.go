//go:build mock_balances

package app

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/x/evm/state"
)

// mockTopOffBalance mirrors the StateDB auto-top-off (state.DBImpl.ensureMinimumBalance)
// on the mempool's committed-balance read path.
//
// The sei-tendermint mempool gates EVM-tx readiness on committed balance, which it
// reads via App.EvmBalance -> EvmKeeper.GetBalance. mock_balances only inflates the
// ephemeral StateDB (GetBalance/SubBalance), so without this a freshly-generated load
// account reads as 0 on the gate path and its txs are never marked ready — the chain
// produces empty blocks while eth_getBalance shows the account funded. Reporting at
// least TopOffAmount here matches what StateDB will grant at execution time.
// TESTING/BENCH ONLY (mock_balances is never built for mainnet).
func mockTopOffBalance(balance *big.Int) *big.Int {
	if balance.Cmp(state.TopOffAmount) < 0 {
		return new(big.Int).Set(state.TopOffAmount)
	}
	return balance
}
