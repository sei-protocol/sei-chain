package state

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *StateDBImpl) CreateAccount(common.Address) {
	// noop
	// EVM account creation is handled in ante handlers.
	// State initialization is handled in Get/SetState
}

func (s *StateDBImpl) SubBalance(evmAddr common.Address, amt *big.Int) {
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.AddBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}
	defer func() {
		s.deficit = new(big.Int).Sub(s.deficit, amt)
	}()

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

	s.k.SetOrDeleteBalance(s.ctx, evmAddr, balance-amt.Uint64())
}

func (s *StateDBImpl) AddBalance(evmAddr common.Address, amt *big.Int) {
	if amt.Sign() == 0 {
		return
	}
	if amt.Sign() < 0 {
		s.SubBalance(evmAddr, new(big.Int).Neg(amt))
		return
	}
	defer func() {
		s.deficit = new(big.Int).Add(s.deficit, amt)
	}()

	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
		// credit seiAddr's bank balance and debit EVM module account, mint if needed
		coin := sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(amt))
		coins := sdk.NewCoins(coin)
		moduleAccAddr := s.k.AccountKeeper().GetModuleAddress(types.ModuleName)
		if !s.k.BankKeeper().HasBalance(s.ctx, moduleAccAddr, coin) {
			s.err = s.k.BankKeeper().MintCoins(s.ctx, types.ModuleName, coins)
			if s.err != nil {
				return
			}
			s.minted = new(big.Int).Add(s.minted, amt)
		}
		s.err = s.k.BankKeeper().SendCoinsFromModuleToAccount(s.ctx, types.ModuleName, seiAddr, coins)
		return
	}

	balance := s.k.GetBalance(s.ctx, evmAddr)
	if math.MaxUint64-balance < amt.Uint64() {
		s.err = fmt.Errorf("crediting %s to %s with an existing balance of %d would cause overflow", amt, evmAddr, balance)
		return
	}

	s.k.SetOrDeleteBalance(s.ctx, evmAddr, balance+amt.Uint64())
}

func (s *StateDBImpl) GetBalance(evmAddr common.Address) *big.Int {
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, evmAddr); ok {
		return s.k.BankKeeper().GetBalance(s.ctx, seiAddr, s.k.GetBaseDenom(s.ctx)).Amount.BigInt()
	}
	return big.NewInt(int64(s.k.GetBalance(s.ctx, evmAddr)))
}

func (s *StateDBImpl) CheckBalance() error {
	if s.err != nil {
		return errors.New("should not call CheckBalance if there is already an error during execution")
	}
	currentModuleBalance := s.k.GetModuleBalance(s.ctx)
	effectiveCurrentModuleBalance := new(big.Int).Sub(currentModuleBalance, s.minted)
	expectedCurrentModuleBalance := new(big.Int).Sub(s.initialModuleBalance, s.deficit)
	if effectiveCurrentModuleBalance.Cmp(expectedCurrentModuleBalance) != 0 {
		return fmt.Errorf("balance check failed. Initial balance: %s, current balance: %s, minted: %s, deficit: %s", s.initialModuleBalance, currentModuleBalance, s.minted, s.deficit)
	}
	// burn any minted token. If the function errors before, the state would be rolled back anyway
	if err := s.k.BankKeeper().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromBigInt(s.minted)))); err != nil {
		return err
	}
	return nil
}
