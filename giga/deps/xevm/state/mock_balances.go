//go:build mock_balances

package state

/*
==============================================================================
============================= !!! WARNING !!! ================================
==============================================================================
== This file is ONLY for TESTING/BENCHMARKING.                              ==
== It enables automatic top-off of EVM accounts with insufficient funds.    ==
== DO NOT USE IN PRODUCTION OR MAINNET BUILDS.                              ==
== This is enabled only when the 'mock_balances' build tag is set.          ==
==============================================================================
*/

import (
	"math/big"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// TopOffAmount is the amount to mint when an account needs more funds (100 ETH)
var TopOffAmount = new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000))

// ensureMinimumBalance tops off the account if balance is low.
// Called from GetBalance to ensure preCheck passes in StateTransition.
func (s *DBImpl) ensureMinimumBalance(evmAddr common.Address) {
	if s.ctx.ChainID() == "pacific-1" {
		panic("FATAL: mock_balances build tag enabled on pacific-1 mainnet - this is a critical misconfiguration")
	}

	seiAddr := s.getSeiAddress(evmAddr)
	currentBalance := s.k.GetBalance(s.ctx, seiAddr)

	if currentBalance.Cmp(TopOffAmount) < 0 {
		s.topOffAccount(seiAddr, TopOffAmount)
	}
}

// ensureSufficientBalance tops off the account if it doesn't have enough for the operation.
// Called from SubBalance before actually subtracting.
func (s *DBImpl) ensureSufficientBalance(evmAddr common.Address, amt *big.Int) {
	seiAddr := s.getSeiAddress(evmAddr)
	currentBalance := s.k.GetBalance(s.ctx, seiAddr)

	if currentBalance.Cmp(amt) < 0 {
		needed := new(big.Int).Sub(amt, currentBalance)
		needed = needed.Add(needed, TopOffAmount)
		s.topOffAccount(seiAddr, needed)
	}
}

// topOffAccount mints funds to an account.
func (s *DBImpl) topOffAccount(seiAddr sdk.AccAddress, amt *big.Int) {
	// Ensure account exists
	if !s.k.AccountKeeper().HasAccount(s.ctx, seiAddr) {
		s.k.AccountKeeper().SetAccount(s.ctx, s.k.AccountKeeper().NewAccountWithAddress(s.ctx, seiAddr))
	}

	// Mint and send (use NopLogger to suppress log spam)
	usei, wei := SplitUseiWeiAmount(amt)
	coinsAmt := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei.Add(sdk.OneInt())))
	ctx := s.ctx.WithLogger(log.NewNopLogger())
	if err := s.k.BankKeeper().MintCoins(ctx, types.ModuleName, coinsAmt); err != nil {
		return
	}
	moduleAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
	_ = s.k.BankKeeper().SendCoinsAndWei(ctx, moduleAddr, seiAddr, usei, wei)
}
