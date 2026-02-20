package migrations

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func RemoveTxHashes(ctx sdk.Context, k *keeper.Keeper) error {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.TxHashesPrefix)
	return store.DeleteAll(nil, nil)
}
