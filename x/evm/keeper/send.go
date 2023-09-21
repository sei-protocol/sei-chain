package keeper

import (
	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) EVMToEVMSend(ctx sdk.Context, fromAddress common.Address, toAddress common.Address, amount uint64) error {
	if amount == 0 {
		return nil
	}
	if err := k.DebitAddress(ctx, fromAddress, amount); err != nil {
		return err
	}
	return k.CreditAddress(ctx, toAddress, amount)
}

func (k *Keeper) BankToEVMSend(ctx sdk.Context, bankAddress sdk.AccAddress, toAddress common.Address, amount uint64) error {
	if amount == 0 {
		return nil
	}
	// first transfer tokens to evm module account
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, bankAddress, types.ModuleName,
		sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromUint64(amount)))); err != nil {
		return err
	}
	// then credit the recipient address
	return k.CreditAddress(ctx, toAddress, amount)
}

func (k *Keeper) EVMToBankSend(ctx sdk.Context, evmAddress common.Address, toAddress sdk.AccAddress, amount uint64) error {
	if amount == 0 {
		return nil
	}
	// first transfer tokens out of evm module account
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, toAddress,
		sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromUint64(amount)))); err != nil {
		return err
	}
	// then debit the sender address
	return k.DebitAddress(ctx, evmAddress, amount)
}

func (k *Keeper) CreditAddress(ctx sdk.Context, address common.Address, amount uint64) error {
	if amount == 0 {
		return nil
	}
	balance := k.GetBalance(ctx, address)
	if math.MaxUint64-balance < amount {
		return fmt.Errorf("crediting %d to %s which has a balance of %d would cause overflow", amount, address, balance)
	}
	k.SetOrDeleteBalance(ctx, address, balance+amount)
	return nil
}

func (k *Keeper) DebitAddress(ctx sdk.Context, address common.Address, amount uint64) error {
	if amount == 0 {
		return nil
	}
	balance := k.GetBalance(ctx, address)
	if balance < amount {
		return sdkerrors.ErrInsufficientFunds
	}
	k.SetOrDeleteBalance(ctx, address, balance-amount)
	return nil
}
