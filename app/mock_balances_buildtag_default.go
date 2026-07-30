//go:build !mock_balances

package app

import "math/big"

// MockBalancesEnabled indicates whether mock_balances build tag is set.
const MockBalancesEnabled = false

// mempoolBalanceFloor is the identity in production builds: the mempool
// readiness gate sees the real check-state balance.
func mempoolBalanceFloor(balance *big.Int) *big.Int {
	return balance
}
