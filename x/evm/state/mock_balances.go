//go:build mock_balances

package state

/*
==============================================================================
============================= !!! WARNING !!! ================================
==============================================================================
== This file is ONLY for TESTING PURPOSES.                                  ==
== It enables MOCK BALANCES for EVM accounts.                               ==
== DO NOT USE IN PRODUCTION OR MAINNET BUILDS.                              ==
== This file is included only when the 'mock_balances' build tag is set,    ==
== and replaces the existing balance.go file.                               ==
== It tops off balances when accounts have insufficient funds.              ==
==============================================================================
*/

import (
	"math/big"

	"github.com/tendermint/tendermint/libs/log"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var ZeroInt = uint256.NewInt(0)

// TopOffAmount is the amount to mint when an account needs more funds (100 ETH)
var TopOffAmount = new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000))

func (s *DBImpl) SubBalance(evmAddr common.Address, amtUint256 *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	amt := amtUint256.ToBig()
	if amt.Sign() == 0 {
		return *ZeroInt
	}
	if amt.Sign() < 0 {
		return s.AddBalance(evmAddr, new(uint256.Int).Neg(amtUint256), reason)
	}

	ctx := s.ctx
	if s.eventsSuppressed {
		ctx = ctx.WithEventManager(sdk.NewEventManager())
	}

	// Always ensure account has enough - add coins directly via bank keeper
	// This avoids cache visibility issues by using the same ctx for both add and sub
	seiAddr := s.getSeiAddress(evmAddr)
	currentBalance := s.k.GetBalance(s.ctx, seiAddr)
	if currentBalance.Cmp(amt) < 0 {
		// Need to top off - add the difference plus extra buffer directly
		needed := new(big.Int).Sub(amt, currentBalance)
		needed = needed.Add(needed, TopOffAmount)
		neededUsei, neededWei := SplitUseiWeiAmount(needed)

		// Ensure account exists
		if !s.k.AccountKeeper().HasAccount(s.ctx, seiAddr) {
			s.k.AccountKeeper().SetAccount(s.ctx, s.k.AccountKeeper().NewAccountWithAddress(s.ctx, seiAddr))
		}

		// Add coins directly (this mints internally)
		coinsAmt := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), neededUsei.Add(sdk.OneInt())))
		if err := s.k.BankKeeper().MintCoins(ctx.WithLogger(log.NewNopLogger()), types.ModuleName, coinsAmt); err == nil {
			moduleAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
			_ = s.k.BankKeeper().SendCoinsAndWei(ctx, moduleAddr, seiAddr, neededUsei, neededWei)
		}
	}

	usei, wei := SplitUseiWeiAmount(amt)
	addr := s.getSeiAddress(evmAddr)
	err := s.k.BankKeeper().SubUnlockedCoins(ctx, addr, sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei)), true)
	if err != nil {
		s.err = err
		return *ZeroInt
	}
	err = s.k.BankKeeper().SubWei(ctx, addr, wei)
	if err != nil {
		s.err = err
		return *ZeroInt
	}

	if s.logger != nil && s.logger.OnBalanceChange != nil {
		newBalance := s.GetBalance(evmAddr).ToBig()
		oldBalance := new(big.Int).Add(newBalance, amt)
		s.logger.OnBalanceChange(evmAddr, oldBalance, newBalance, reason)
	}

	s.tempState.surplus = s.tempState.surplus.Add(sdk.NewIntFromBigInt(amt))
	return *ZeroInt
}

func (s *DBImpl) AddBalance(evmAddr common.Address, amtUint256 *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	amt := amtUint256.ToBig()
	if amt.Sign() == 0 {
		return *ZeroInt
	}
	if amt.Sign() < 0 {
		return s.SubBalance(evmAddr, new(uint256.Int).Neg(amtUint256), reason)
	}

	ctx := s.ctx
	if s.eventsSuppressed {
		ctx = ctx.WithEventManager(sdk.NewEventManager())
	}

	usei, wei := SplitUseiWeiAmount(amt)
	addr := s.getSeiAddress(evmAddr)
	err := s.k.BankKeeper().AddCoins(ctx, addr, sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei)), true)
	if err != nil {
		s.err = err
		return *ZeroInt
	}
	err = s.k.BankKeeper().AddWei(ctx, addr, wei)
	if err != nil {
		s.err = err
		return *ZeroInt
	}

	if s.logger != nil && s.logger.OnBalanceChange != nil {
		newBalance := s.GetBalance(evmAddr).ToBig()
		oldBalance := new(big.Int).Sub(newBalance, amt)
		s.logger.OnBalanceChange(evmAddr, oldBalance, newBalance, reason)
	}

	s.tempState.surplus = s.tempState.surplus.Sub(sdk.NewIntFromBigInt(amt))
	return *ZeroInt
}

// PrepareMockBalance is kept for API compatibility but is now a no-op.
// GetBalance handles all top-off logic automatically.
func (s *DBImpl) PrepareMockBalance(_ common.Address) {
	// No-op: GetBalance now handles top-off automatically
}

func (s *DBImpl) GetBalance(evmAddr common.Address) *uint256.Int {
	// SAFETY: Never allow mock balances on mainnet
	if s.ctx.ChainID() == "pacific-1" {
		panic("FATAL: mock_balances build tag enabled on pacific-1 mainnet - this is a critical misconfiguration")
	}

	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	seiAddr := s.getSeiAddress(evmAddr)
	currentBalance := s.k.GetBalance(s.ctx, seiAddr)

	// If balance is low, top it off immediately
	// This ensures preCheck passes in StateTransition
	if currentBalance.Cmp(TopOffAmount) < 0 {
		s.topOffAccount(seiAddr)
		// Re-read the balance after top-off
		currentBalance = s.k.GetBalance(s.ctx, seiAddr)
	}

	res, overflow := uint256.FromBig(currentBalance)
	if overflow {
		panic("balance overflow")
	}
	if res == nil {
		return uint256.NewInt(0)
	}
	return res
}

// topOffAccount mints funds to an account that has low balance
func (s *DBImpl) topOffAccount(seiAddr sdk.AccAddress) {
	// Ensure account exists
	if !s.k.AccountKeeper().HasAccount(s.ctx, seiAddr) {
		s.k.AccountKeeper().SetAccount(s.ctx, s.k.AccountKeeper().NewAccountWithAddress(s.ctx, seiAddr))
	}

	// Mint and send top-off amount (use NopLogger to suppress log spam)
	usei, wei := SplitUseiWeiAmount(TopOffAmount)
	coinsAmt := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei.Add(sdk.OneInt())))
	ctx := s.ctx.WithLogger(log.NewNopLogger())
	if err := s.k.BankKeeper().MintCoins(ctx, types.ModuleName, coinsAmt); err != nil {
		return
	}
	moduleAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
	_ = s.k.BankKeeper().SendCoinsAndWei(ctx, moduleAddr, seiAddr, usei, wei)
}

// should only be called during simulation
func (s *DBImpl) SetBalance(evmAddr common.Address, amtUint256 *uint256.Int, reason tracing.BalanceChangeReason) {
	if !s.simulation {
		panic("should never call SetBalance in a non-simulation setting")
	}
	amt := amtUint256.ToBig()
	seiAddr := s.getSeiAddress(evmAddr)
	moduleAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
	s.send(seiAddr, moduleAddr, s.GetBalance(evmAddr).ToBig())
	if s.err != nil {
		panic(s.err)
	}
	usei, _ := SplitUseiWeiAmount(amt)
	coinsAmt := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei.Add(sdk.OneInt())))
	if err := s.k.BankKeeper().MintCoins(s.ctx, types.ModuleName, coinsAmt); err != nil {
		panic(err)
	}
	s.send(moduleAddr, seiAddr, amt)
	if s.err != nil {
		panic(s.err)
	}
}

func (s *DBImpl) getSeiAddress(evmAddr common.Address) sdk.AccAddress {
	if s.coinbaseEvmAddress.Cmp(evmAddr) == 0 {
		return s.coinbaseAddress
	}
	return s.k.GetSeiAddressOrDefault(s.ctx, evmAddr)
}

func (s *DBImpl) send(from sdk.AccAddress, to sdk.AccAddress, amt *big.Int) {
	usei, wei := SplitUseiWeiAmount(amt)
	err := s.k.BankKeeper().SendCoinsAndWei(s.ctx, from, to, usei, wei)
	if err != nil {
		s.err = err
	}
}
