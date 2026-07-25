package keeper

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// MaxBlockHashHistory is the number of recent block hashes retained for BLOCKHASH
// (Yellow Paper / EVM: only the previous 256 blocks are available).
const MaxBlockHashHistory = 256

func (k *Keeper) GetBlockHash(ctx sdk.Context, height int64) (common.Hash, bool) {
	store := ctx.KVStore(k.GetStoreKey())
	bz := store.Get(types.BlockHashKey(height))
	if len(bz) == 0 {
		return common.Hash{}, false
	}
	return common.BytesToHash(bz), true
}

func (k *Keeper) SetBlockHash(ctx sdk.Context, height int64, hash common.Hash) {
	store := ctx.KVStore(k.GetStoreKey())
	store.Set(types.BlockHashKey(height), hash.Bytes())
}

func (k *Keeper) DeleteBlockHash(ctx sdk.Context, height int64) {
	store := ctx.KVStore(k.GetStoreKey())
	store.Delete(types.BlockHashKey(height))
}

// PruneBlockHashCache drops the in-memory entry that fell out of MaxBlockHashHistory.
// The KV ring is maintained by EvmKeeper.TrackBlockHash (shared store).
func (k *Keeper) PruneBlockHashCache(ctx sdk.Context) {
	if prune := ctx.BlockHeight() - 1 - MaxBlockHashHistory; prune >= 0 {
		k.blockHashCache.Delete(uint64(prune)) //nolint:gosec
	}
}
