package state

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) SubBalance(evmAddr common.Address, amt *big.Int) {
	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.AddBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}

	usei, wei := SplitUseiWeiAmount(amt)
	addr := s.getSeiAddress(evmAddr)
	s.err = s.k.BankKeeper().SubUnlockedCoins(s.ctx, addr, sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei)), true)
	if s.err != nil {
		return
	}
	s.err = s.k.BankKeeper().SubWei(s.ctx, addr, wei)
	if s.err != nil {
		return
	}
	s.tempStateCurrent.surplus = s.tempStateCurrent.surplus.Add(sdk.NewIntFromBigInt(amt))
}

func (s *DBImpl) AddBalance(evmAddr common.Address, amt *big.Int) {
	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.SubBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}

	usei, wei := SplitUseiWeiAmount(amt)
	addr := s.getSeiAddress(evmAddr)
	s.err = s.k.BankKeeper().AddCoins(s.ctx, addr, sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), usei)), true)
	if s.err != nil {
		return
	}
	s.err = s.k.BankKeeper().AddWei(s.ctx, addr, wei)
	if s.err != nil {
		return
	}
	s.tempStateCurrent.surplus = s.tempStateCurrent.surplus.Sub(sdk.NewIntFromBigInt(amt))
}

func (s *DBImpl) GetBalance(evmAddr common.Address) *big.Int {
	s.k.PrepareReplayedAddr(s.ctx, evmAddr)
	usei := s.k.BankKeeper().GetBalance(s.ctx, s.getSeiAddress(evmAddr), s.k.GetBaseDenom(s.ctx)).Amount
	wei := s.k.BankKeeper().GetWeiBalance(s.ctx, s.getSeiAddress(evmAddr))
	return usei.Mul(SdkUseiToSweiMultiplier).Add(wei).BigInt()
}

// should only be called during simulation
func (s *DBImpl) SetBalance(evmAddr common.Address, amt *big.Int) {
	if !s.simulation {
		panic("should never call SetBalance in a non-simulation setting")
	}
	seiAddr := s.getSeiAddress(evmAddr)
	moduleAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
	s.send(seiAddr, moduleAddr, s.GetBalance(evmAddr))
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
	if feeCollector, _ := s.k.GetFeeCollectorAddress(s.ctx); feeCollector == evmAddr {
		return s.coinbaseAddress
	}
	return s.k.GetSeiAddressOrDefault(s.ctx, evmAddr)
}

func (s *DBImpl) send(from sdk.AccAddress, to sdk.AccAddress, amt *big.Int) {
	usei, wei := SplitUseiWeiAmount(amt)
	s.err = s.k.BankKeeper().SendCoinsAndWei(s.ctx, from, to, usei, wei)
}
