package state

import (
	"math/big"

	"github.com/tendermint/tendermint/libs/log"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// This is a compile time flag to enable/disable the mock balance function.
// It is only intended for use in TESTING.
// go build -X github.com/sei-protocol/sei-chain/x/evm/state.MockBalanceTesting=true
var mockBalanceTesting string = ""

var ZeroInt = uint256.NewInt(0)

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

	// this avoids emitting cosmos events for ephemeral bookkeeping transfers like send_native
	if s.eventsSuppressed {
		ctx = ctx.WithEventManager(sdk.NewEventManager())
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
		// We could modify AddWei instead so it returns us the old/new balance directly.
		newBalance := s.GetBalance(evmAddr).ToBig()
		oldBalance := new(big.Int).Add(newBalance, amt)

		s.logger.OnBalanceChange(evmAddr, oldBalance, newBalance, reason)
	}

	s.tempStateCurrent.surplus = s.tempStateCurrent.surplus.Add(sdk.NewIntFromBigInt(amt))
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
	// this avoids emitting cosmos events for ephemeral bookkeeping transfers like send_native
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
		// We could modify AddWei instead so it returns us the old/new balance directly.
		newBalance := s.GetBalance(evmAddr).ToBig()
		oldBalance := new(big.Int).Sub(newBalance, amt)

		s.logger.OnBalanceChange(evmAddr, oldBalance, newBalance, reason)
	}

	s.tempStateCurrent.surplus = s.tempStateCurrent.surplus.Sub(sdk.NewIntFromBigInt(amt))
	return *ZeroInt
}

// This function is used to mock balance, and is only intended for use in TESTING. The access to this function will be gated by a compile time flag.
func (s *DBImpl) mockBalance(evmAddr common.Address) *uint256.Int {
	if mockBalanceTesting != "enabled" {
		panic("mockBalance is only intended for use in TESTING")
	}
	// Prevent calling mockBalance on sei mainnet
	if config.GetEVMChainID(s.ctx.ChainID()) == big.NewInt(1329) {
		panic("Prevent mock balance from ever being called on mainnet")
	}

	// Mint enough for many gas operations (10 ETH worth)
	bal := uint256.NewInt(10_000_000_000_000_000_000) // 10 ETH in wei

	amt := bal.ToBig()
	seiAddr := s.getSeiAddress(evmAddr)

	// Check if account exists, create if needed
	if !s.k.AccountKeeper().HasAccount(s.ctx, seiAddr) {
		s.k.AccountKeeper().SetAccount(s.ctx, s.k.AccountKeeper().NewAccountWithAddress(s.ctx, seiAddr))
	}

	moduleAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
	usei, _ := SplitUseiWeiAmount(amt)
	coinsAmt := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei.Add(sdk.OneInt())))

	// avoids flooding logs
	if err := s.k.BankKeeper().MintCoins(s.ctx.WithLogger(log.NewNopLogger()), types.ModuleName, coinsAmt); err != nil {
		panic(err)
	}
	s.send(moduleAddr, seiAddr, amt)
	if s.err != nil {
		panic(s.err)
	}
	return bal
}

func (s *DBImpl) GetBalance(evmAddr common.Address) *uint256.Int {
	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	seiAddr := s.getSeiAddress(evmAddr)
	res, overflow := uint256.FromBig(s.k.GetBalance(s.ctx, seiAddr))
	if overflow {
		panic("balance overflow")
	}
	if mockBalanceTesting == "enabled" {
		// Lazy initialization: if balance is insufficient for gas operations, mint more
		minRequiredBalance := uint256.NewInt(1_000_000_000_000_000_000) // 1 ETH worth of wei for gas
		if res == nil || res.Cmp(minRequiredBalance) < 0 {
			mockBal := s.mockBalance(evmAddr)
			return mockBal
		}
	}
	if res == nil {
		return uint256.NewInt(0)
	}
	return res
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
