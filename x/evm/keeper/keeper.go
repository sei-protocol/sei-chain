package keeper

import (
	"math/big"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type Keeper struct {
	storeKey   sdk.StoreKey
	Paramstore paramtypes.Subspace

	evmChainID *big.Int

	bankKeeper    bankkeeper.Keeper
	accountKeeper *authkeeper.AccountKeeper
	stakingKeeper *stakingkeeper.Keeper
}

func NewKeeper(
	storeKey sdk.StoreKey, paramstore paramtypes.Subspace, evmChainID *big.Int,
	bankKeeper bankkeeper.Keeper, accountKeeper *authkeeper.AccountKeeper, stakingKeeper *stakingkeeper.Keeper) *Keeper {
	if !paramstore.HasKeyTable() {
		paramstore = paramstore.WithKeyTable(types.ParamKeyTable())
	}
	return &Keeper{
		storeKey:      storeKey,
		Paramstore:    paramstore,
		evmChainID:    evmChainID,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
		stakingKeeper: stakingKeeper,
	}
}

func (k *Keeper) ChainID() *big.Int {
	return k.evmChainID
}

func (k *Keeper) AccountKeeper() *authkeeper.AccountKeeper {
	return k.accountKeeper
}

func (k *Keeper) BankKeeper() bankkeeper.Keeper {
	return k.bankKeeper
}

func (k *Keeper) GetModuleBalance(ctx sdk.Context) *big.Int {
	return k.BankKeeper().GetBalance(ctx, k.AccountKeeper().GetModuleAddress(types.ModuleName), k.GetBaseDenom(ctx)).Amount.BigInt()
}

func (k *Keeper) GetStoreKey() sdk.StoreKey {
	return k.storeKey
}

func (k *Keeper) PrefixStore(ctx sdk.Context, pref []byte) sdk.KVStore {
	store := ctx.KVStore(k.GetStoreKey())
	return prefix.NewStore(store, pref)
}

func (k *Keeper) PurgePrefix(ctx sdk.Context, pref []byte) {
	store := k.PrefixStore(ctx, pref)
	iter := store.Iterator(nil, nil)
	keys := [][]byte{}
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	iter.Close()
	for _, key := range keys {
		store.Delete(key)
	}
}
