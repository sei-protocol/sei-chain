package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetBalance(ctx sdk.Context, address common.Address) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.BalanceKey(address))
	if bz == nil {
		return 0
	}
	balance := sdk.Coin{}
	if err := balance.Unmarshal(bz); err != nil {
		// this should never happen
		panic(err)
	}
	return balance.Amount.Uint64()
}

func (k *Keeper) SetOrDeleteBalance(ctx sdk.Context, address common.Address, amount uint64) {
	store := ctx.KVStore(k.storeKey)
	if amount == 0 {
		k.deleteBalance(ctx, address)
		return
	}
	balance := sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewIntFromUint64(amount))
	bz, _ := balance.Marshal()
	store.Set(types.BalanceKey(address), bz)
}

func (k *Keeper) deleteBalance(ctx sdk.Context, address common.Address) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.BalanceKey(address))
}
