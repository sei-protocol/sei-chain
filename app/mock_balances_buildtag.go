//go:build mock_balances

package app

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/x/evm/state"
)

// MockBalancesEnabled indicates whether mock_balances build tag is set.
const MockBalancesEnabled = true

// mempoolBalanceFloor lifts the balance the mempool readiness gate sees to at
// least the mock top-off amount. The CheckTx top-off writes to a discarded
// cache, so the check-state balance behind EvmBalance never reflects it —
// without this floor a mock-funded sender never satisfies requiredBalance,
// its txs are never promoted to ready, and ready-only gossip/reaping means
// they can never reach a block. Execution is unaffected: the real top-off
// runs in the delivery path, where writes persist.
func mempoolBalanceFloor(balance *big.Int) *big.Int {
	if balance.Cmp(state.TopOffAmount) < 0 {
		return new(big.Int).Set(state.TopOffAmount)
	}
	return balance
}
