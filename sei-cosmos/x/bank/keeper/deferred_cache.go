package keeper

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
)

type DeferredCache struct {
	storeKey sdk.StoreKey
	cdc      codec.BinaryCodec
}

func NewDeferredCache(cdc codec.BinaryCodec, storeKey sdk.StoreKey) *DeferredCache {
	return &DeferredCache{
		cdc:      cdc,
		storeKey: storeKey,
	}
}

func (d *DeferredCache) getModuleTxIndexedStore(ctx sdk.Context, moduleAddr sdk.AccAddress, txIndex uint64) prefix.Store {
	store := ctx.KVStore(d.storeKey)

	return prefix.NewStore(store, types.CreateDeferredCacheModuleTxIndexedPrefix(moduleAddr, txIndex))
}

// GetBalance returns the balance of a specific denomination for a given module address and transaction index
func (d *DeferredCache) GetBalance(ctx sdk.Context, moduleAddr sdk.AccAddress, txIndex uint64, denom string) sdk.Coin {
	deferredStore := d.getModuleTxIndexedStore(ctx, moduleAddr, txIndex)

	bz := deferredStore.Get([]byte(denom))
	if bz == nil {
		return sdk.NewCoin(denom, sdk.ZeroInt())
	}

	var balance sdk.Coin
	d.cdc.MustUnmarshal(bz, &balance)

	return balance
}

// setBalance sets the coin balance for a module and tx Index.
func (d *DeferredCache) setBalance(ctx sdk.Context, moduleAddr sdk.AccAddress, txIndex uint64, balance sdk.Coin) error {
	if !balance.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, balance.String())
	}

	deferredStore := d.getModuleTxIndexedStore(ctx, moduleAddr, txIndex)
	// Bank invariants require to not store zero balances, so we follow the same pattern in deferred cache.
	if balance.IsZero() {
		deferredStore.Delete([]byte(balance.Denom))
	} else {
		bz := d.cdc.MustMarshal(&balance)
		deferredStore.Set([]byte(balance.Denom), bz)
	}
	return nil
}

// upsertBalance updates or sets the coin balance for a module and tx combination keyed on balance denom.
func (d *DeferredCache) upsertBalance(ctx sdk.Context, moduleAddr sdk.AccAddress, txIndex uint64, balance sdk.Coin) error {
	if !balance.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, balance.String())
	}

	currBalance := d.GetBalance(ctx, moduleAddr, txIndex, balance.Denom)
	newBalance := currBalance.Add(balance)

	return d.setBalance(ctx, moduleAddr, txIndex, newBalance)
}

// UpsertBalances updates or sets the coin balances for a module and tx combination with the given coins.
func (d *DeferredCache) UpsertBalances(ctx sdk.Context, moduleAddr sdk.AccAddress, txIndex uint64, balances sdk.Coins) error {
	if !balances.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, balances.String())
	}

	// iterate through coins and upsert
	for _, coin := range balances {
		err := d.upsertBalance(ctx, moduleAddr, txIndex, coin)
		if err != nil {
			return err
		}
	}
	return nil
}

// IterateAccountBalances iterates over the balances of a single module for all tx indices and
// provides the token balance to a callback.
// Note that because there can be multiple tx indices per module,
// there can be multiple occurrences of the same denom in `balance`.
// If true is returned from the
// callback, iteration is halted.
func (d *DeferredCache) IterateDeferredBalances(ctx sdk.Context, cb func(moduleAddr sdk.AccAddress, balance sdk.Coin) bool) {
	deferredStore := prefix.NewStore(ctx.KVStore(d.storeKey), types.DeferredCachePrefix)

	iterator := deferredStore.Iterator(nil, nil)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var balance sdk.Coin
		d.cdc.MustUnmarshal(iterator.Value(), &balance)
		moduleAddr, err := types.AddressFromDeferredCacheStore(iterator.Key())
		if err != nil {
			ctx.Logger().With("key", iterator.Key(), "err", err).Error("failed to get address from deferred cache store")
			panic(err)
		}

		if cb(moduleAddr, balance) {
			break
		}
	}
}

// Clear deletes all of the keys in the deferred cache
func (d *DeferredCache) Clear(ctx sdk.Context) {
	store := prefix.NewStore(ctx.KVStore(d.storeKey), types.DeferredCachePrefix)

	iterator := store.Iterator(nil, nil)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		store.Delete(iterator.Key())
	}
}
