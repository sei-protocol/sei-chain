package keeper

import (
	"github.com/ethereum/go-ethereum/common"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// MaxBlockHashHistory is the number of recent block hashes retained for BLOCKHASH
// (Yellow Paper / EVM: only the previous 256 blocks are available).
const MaxBlockHashHistory = 256

// blockHashCacheSize bounds the process-local BLOCKHASH cache (tip window plus headroom).
const blockHashCacheSize = MaxBlockHashHistory * 2

func newBlockHashCache() *lru.Cache[uint64, common.Hash] {
	c, err := lru.New[uint64, common.Hash](blockHashCacheSize)
	if err != nil {
		panic(err)
	}
	return c
}

// Block-hash ring accessors use ctx.KVStore (not GetKVStore / GigaKVStore) so
// they read the same store EvmKeeper.TrackBlockHash writes on BeginBlock.
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

// cacheBlockHash stores a BLOCKHASH result for DeliverTx execution only.
// CheckTx/recheck and RPC/trace contexts may read the cache but do not insert.
func (k *Keeper) cacheBlockHash(ctx sdk.Context, height uint64, hash common.Hash) {
	if hash == (common.Hash{}) {
		return
	}
	if ctx.IsCheckTx() || ctx.IsReCheckTx() || ctx.IsTracing() {
		return
	}
	k.blockHashCache.Add(height, hash)
}
