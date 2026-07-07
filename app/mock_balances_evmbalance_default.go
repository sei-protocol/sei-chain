//go:build !mock_balances

package app

import "math/big"

// mockTopOffBalance is a no-op in production builds.
func mockTopOffBalance(balance *big.Int) *big.Int { return balance }
