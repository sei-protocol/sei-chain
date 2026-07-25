package keeper

import (
	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

// TrackBlockHash stores LastBlockId.Hash for height-1 and prunes the height
// that fell out of MaxBlockHashHistory from KV and the in-memory cache.
func (k *Keeper) TrackBlockHash(ctx sdk.Context) {
	height := ctx.BlockHeight()
	if height <= 0 {
		return
	}
	parent := common.BytesToHash(ctx.BlockHeader().LastBlockId.Hash)
	if parent != (common.Hash{}) {
		k.SetBlockHash(ctx, height-1, parent)
	}
	if prune := height - 1 - MaxBlockHashHistory; prune >= 0 {
		k.DeleteBlockHash(ctx, prune)
		k.blockHashCache.Delete(uint64(prune)) //nolint:gosec
	}
}
