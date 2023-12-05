package state

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) SubBalance(evmAddr common.Address, amt *big.Int) {
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.AddBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}

	s.err = s.k.BankKeeper().SendCoinsWithoutAccCreation(s.ctx, s.getSeiAddress(evmAddr), s.middleManAddress, s.bigIntAmtToCoins(amt))
}

func (s *DBImpl) AddBalance(evmAddr common.Address, amt *big.Int) {
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.SubBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}

	s.err = s.k.BankKeeper().SendCoinsWithoutAccCreation(s.ctx, s.middleManAddress, s.getSeiAddress(evmAddr), s.bigIntAmtToCoins(amt))
}

func (s *DBImpl) GetBalance(evmAddr common.Address) *big.Int {
	return s.coinToBigIntAmt(s.k.BankKeeper().GetBalance(s.ctx, s.getSeiAddress(evmAddr), s.k.GetBaseDenom(s.ctx)))
}

// should only be called during simulation
func (s *DBImpl) SetBalance(evmAddr common.Address, amt *big.Int) {
	if !s.simulation {
		panic("should never call SetBalance in a non-simulation setting")
	}
	// Fields that were denominated in usei will be converted to swei (1usei = 10^12swei)
	// for existing Ethereum application (which assumes 18 decimal points) to display properly.
	amt = new(big.Int).Quo(amt, UseiToSweiMultiplier)
	seiAddr := s.getSeiAddress(evmAddr)
	balance := s.k.BankKeeper().GetBalance(s.ctx, seiAddr, s.k.GetBaseDenom(s.ctx))
	if err := s.k.BankKeeper().SendCoinsFromAccountToModule(s.ctx, seiAddr, types.ModuleName, sdk.NewCoins(balance)); err != nil {
		panic(err)
	}
	coinsAmt := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt)))
	if err := s.k.BankKeeper().MintCoins(s.ctx, types.ModuleName, coinsAmt); err != nil {
		panic(err)
	}
	if err := s.k.BankKeeper().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, seiAddr, coinsAmt); err != nil {
		panic(err)
	}
}

func (s *DBImpl) getSeiAddress(evmAddr common.Address) (seiAddr sdk.AccAddress) {
	if feeCollector, _ := s.k.GetFeeCollectorAddress(s.ctx); feeCollector == evmAddr {
		seiAddr = s.coinbaseAddress
	} else if associated, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
		seiAddr = associated
	} else {
		seiAddr = sdk.AccAddress(evmAddr[:])
	}
	return
}

func (s *DBImpl) bigIntAmtToCoins(amt *big.Int) sdk.Coins {
	// Fields that were denominated in usei will be converted to swei (1usei = 10^12swei)
	// for existing Ethereum application (which assumes 18 decimal points) to display properly.
	amt = new(big.Int).Quo(amt, UseiToSweiMultiplier)
	return sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt)))
}

func (s *DBImpl) coinToBigIntAmt(coin sdk.Coin) *big.Int {
	balanceInUsei := coin.Amount.BigInt()
	return new(big.Int).Mul(balanceInUsei, UseiToSweiMultiplier)
}
