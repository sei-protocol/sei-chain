package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (k *Keeper) GetTxHashesOnHeight(ctx sdk.Context, height int64) (res []common.Hash) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.TxHashesKey(height))
	if bz == nil {
		return
	}
	for i := 0; i <= len(bz)-common.HashLength; i += common.HashLength {
		res = append(res, common.Hash(bz[i:i+common.HashLength]))
	}
	return
}

func (k *Keeper) SetTxHashesOnHeight(ctx sdk.Context, height int64, hashes []common.Hash) {
	if len(hashes) == 0 {
		return
	}
	store := ctx.KVStore(k.storeKey)
	bz := []byte{}
	for _, hash := range hashes {
		bz = append(bz, hash[:]...)
	}
	store.Set(types.TxHashesKey(height), bz)
}
