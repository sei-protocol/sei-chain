package keeper

import (
	"github.com/tendermint/tendermint/libs/log"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vestexported "github.com/cosmos/cosmos-sdk/x/auth/vesting/exported"
	"github.com/sei-protocol/sei-chain/giga/deps/xbank/types"
)

var _ ViewKeeper = (*BaseViewKeeper)(nil)

// ViewKeeper defines a module interface that facilitates read only access to
// account balances.
type ViewKeeper interface {
	HasBalance(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Coin) bool

	GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	LockedCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
	SpendableCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins
	GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int
}

// BaseViewKeeper implements a read only keeper implementation of ViewKeeper.
type BaseViewKeeper struct {
	cdc       codec.BinaryCodec
	storeKey  sdk.StoreKey
	ak        types.AccountKeeper
	cacheSize int

	// UseRegularStore when true causes GetKVStore to use ctx.KVStore instead of ctx.GigaKVStore.
	// This is for debugging/testing to isolate Giga executor logic from GigaKVStore layer.
	UseRegularStore bool
}

// NewBaseViewKeeper returns a new BaseViewKeeper.
func NewBaseViewKeeper(cdc codec.BinaryCodec, storeKey sdk.StoreKey, ak types.AccountKeeper) BaseViewKeeper {
	return BaseViewKeeper{
		cdc:             cdc,
		storeKey:        storeKey,
		ak:              ak,
		UseRegularStore: true,
	}
}

// Logger returns a module-specific logger.
func (k BaseViewKeeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// GetKVStore returns the appropriate KVStore based on the UseRegularStore flag.
// When UseRegularStore is true (for debugging/testing), returns regular KVStore.
// Otherwise returns GigaKVStore.
func (k BaseViewKeeper) GetKVStore(ctx sdk.Context) sdk.KVStore {
	if k.UseRegularStore {
		return ctx.KVStore(k.storeKey)
	}
	return ctx.GigaKVStore(k.storeKey)
}

// HasBalance returns whether or not an account has at least amt balance.
func (k BaseViewKeeper) HasBalance(ctx sdk.Context, addr sdk.AccAddress, amt sdk.Coin) bool {
	return k.GetBalance(ctx, addr, amt.Denom).IsGTE(amt)
}

// GetBalance returns the balance of a specific denomination for a given account
// by address.
func (k BaseViewKeeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	accountStore := k.getAccountStore(ctx, addr)

	bz := accountStore.Get([]byte(denom))
	if bz == nil {
		return sdk.NewCoin(denom, sdk.ZeroInt())
	}

	var balance sdk.Coin
	k.cdc.MustUnmarshal(bz, &balance)

	return balance
}

// LockedCoins returns all the coins that are not spendable (i.e. locked) for an
// account by address. For standard accounts, the result will always be no coins.
// For vesting accounts, LockedCoins is delegated to the concrete vesting account
// type.
func (k BaseViewKeeper) LockedCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins {
	acc := k.ak.GetAccount(ctx, addr)
	if acc != nil {
		vacc, ok := acc.(vestexported.VestingAccount)
		if ok {
			return vacc.LockedCoins(ctx.BlockTime())
		}
	}

	return sdk.NewCoins()
}

// IterateAccountBalances iterates over the balances of a single account and
// provides the token balance to a callback. If true is returned from the
// callback, iteration is halted.
func (k BaseViewKeeper) IterateAccountBalances(ctx sdk.Context, addr sdk.AccAddress, cb func(sdk.Coin) bool) {
	accountStore := k.getAccountStore(ctx, addr)

	iterator := accountStore.Iterator(nil, nil)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var balance sdk.Coin
		k.cdc.MustUnmarshal(iterator.Value(), &balance)

		if cb(balance) {
			break
		}
	}
}

// GetAllBalances returns all the balances for a given account address.
func (k BaseViewKeeper) GetAllBalances(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins {
	balances := sdk.NewCoins()
	k.IterateAccountBalances(ctx, addr, func(balance sdk.Coin) bool {
		balances = append(balances, balance)
		return false
	})

	return balances.Sort()
}

// SpendableCoins returns the total balances of spendable coins for an account
// by address. If the account has no spendable coins, an empty Coins slice is
// returned.
func (k BaseViewKeeper) SpendableCoins(ctx sdk.Context, addr sdk.AccAddress) sdk.Coins {
	total := k.GetAllBalances(ctx, addr)
	locked := k.LockedCoins(ctx, addr)

	spendable, hasNeg := total.SafeSub(locked)
	if hasNeg {
		return sdk.NewCoins()
	}
	return spendable
}

// getAccountStore gets the account store of the given address.
func (k BaseViewKeeper) getAccountStore(ctx sdk.Context, addr sdk.AccAddress) prefix.Store {
	store := k.GetKVStore(ctx)

	return prefix.NewStore(store, types.CreateAccountBalancesPrefix(addr))
}

func (k BaseViewKeeper) GetWeiBalance(ctx sdk.Context, addr sdk.AccAddress) sdk.Int {
	store := prefix.NewStore(k.GetKVStore(ctx), types.WeiBalancesPrefix)
	val := store.Get(addr)
	if val == nil {
		return sdk.ZeroInt()
	}
	res := new(sdk.Int)
	if err := res.Unmarshal(val); err != nil {
		// should never happen
		panic(err)
	}
	return *res
}
