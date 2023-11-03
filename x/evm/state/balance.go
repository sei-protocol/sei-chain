package state

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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

	feeCollector, _ := s.k.GetFeeCollectorAddress(s.ctx)
	if feeCollector == evmAddr {
		coins := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt)))
		s.err = s.k.BankKeeper().SendCoinsFromModuleToModule(s.ctx, authtypes.FeeCollectorName, types.ModuleName, coins)
		return
	}

	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
		// debit seiAddr's bank balance and credit EVM module account
		coins := sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt)))
		s.err = s.k.BankKeeper().SendCoinsFromAccountToModule(s.ctx, seiAddr, types.ModuleName, coins)
		return
	}

	balance := s.k.GetBalance(s.ctx, evmAddr)
	if amt.Uint64() > balance {
		s.err = fmt.Errorf("insufficient balance of %d in %s for a %s subtraction", balance, evmAddr, amt)
		return
	}

	s.AddBigIntTransientModuleState(new(big.Int).Neg(amt), TotalUnassociatedBalanceKey)
	s.k.SetOrDeleteBalance(s.ctx, evmAddr, balance-amt.Uint64())
}

func (s *DBImpl) AddBalance(evmAddr common.Address, amt *big.Int) {
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.SubBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}

	feeCollector, _ := s.k.GetFeeCollectorAddress(s.ctx)
	if feeCollector == evmAddr {
		coin := sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt))
		coins := sdk.NewCoins(coin)
		s.err = s.k.BankKeeper().SendCoinsFromModuleToModule(s.ctx, types.ModuleName, authtypes.FeeCollectorName, coins)
		return
	}

	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
		// credit seiAddr's bank balance and debit EVM module account, mint if needed
		coin := sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt))
		coins := sdk.NewCoins(coin)
		moduleAccAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
		if !s.k.BankKeeper().HasBalance(s.ctx, moduleAccAddr, coin) {
			s.err = errors.New("insufficient module balance to facilitate AddBalance")
			return
		}
		s.err = s.k.BankKeeper().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, seiAddr, coins)
		return
	}

	balance := s.k.GetBalance(s.ctx, evmAddr)
	if math.MaxUint64-balance < amt.Uint64() {
		s.err = fmt.Errorf("crediting %s to %s with an existing balance of %d would cause overflow", amt, evmAddr, balance)
		return
	}

	s.AddBigIntTransientModuleState(amt, TotalUnassociatedBalanceKey)
	s.k.SetOrDeleteBalance(s.ctx, evmAddr, balance+amt.Uint64())
}

func (s *DBImpl) GetBalance(evmAddr common.Address) *big.Int {
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
		return s.k.BankKeeper().GetBalance(s.ctx, seiAddr, s.k.GetBaseDenom(s.ctx)).Amount.BigInt()
	}
	return big.NewInt(int64(s.k.GetBalance(s.ctx, evmAddr)))
}

// should only be called during simulation
func (s *DBImpl) SetBalance(evmAddr common.Address, amt *big.Int) {
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
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
		return
	}
	s.k.SetOrDeleteBalance(s.ctx, evmAddr, amt.Uint64())
}

func (s *DBImpl) CheckBalance() error {
	if s.err != nil {
		return errors.New("should not call CheckBalance if there is already an error during execution")
	}
	totalUnassociatedBalance := s.GetBigIntTransientModuleState(TotalUnassociatedBalanceKey)
	currentModuleBalance := s.k.GetModuleBalance(s.ctx)
	if totalUnassociatedBalance.Cmp(currentModuleBalance) > 0 {
		// this means tokens are generated out of thin air during tx processing, which should not happen
		return fmt.Errorf("balance check failed. current balance: %s, total unassociated balance: %s", currentModuleBalance, totalUnassociatedBalance)
	}
	toBeBurned := new(big.Int).Sub(currentModuleBalance, totalUnassociatedBalance)
	// burn any minted token. If the function errors before, the state would be rolled back anyway
	return s.k.BankKeeper().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(toBeBurned))))
}

func (s *DBImpl) AddBigIntTransientModuleState(delta *big.Int, key []byte) {
	store := s.k.PrefixStore(s.ctx, types.TransientModuleStateKeyPrefix)
	old := s.GetBigIntTransientModuleState(key)
	newVal := new(big.Int).Add(old, delta)
	sign := []byte{0}
	if newVal.Sign() < 0 {
		sign = []byte{1}
	}
	store.Set(key, append(sign, newVal.Bytes()...))
}

func (s *DBImpl) GetBigIntTransientModuleState(key []byte) *big.Int {
	store := s.k.PrefixStore(s.ctx, types.TransientModuleStateKeyPrefix)
	bz := store.Get(key)
	if bz == nil {
		return big.NewInt(0)
	}
	res := new(big.Int).SetBytes(bz[1:])
	if bz[0] != 0 {
		res = new(big.Int).Neg(res)
	}
	return res
}
