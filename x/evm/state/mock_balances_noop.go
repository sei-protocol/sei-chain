//go:build !mock_balances

package state

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ensureMinimumBalance is a no-op in production builds.
func (s *DBImpl) ensureMinimumBalance(evmAddr common.Address) {}

// ensureSufficientBalance is a no-op in production builds.
func (s *DBImpl) ensureSufficientBalance(evmAddr common.Address, amt *big.Int) {}
