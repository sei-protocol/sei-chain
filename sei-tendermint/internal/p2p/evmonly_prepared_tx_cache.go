package p2p

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	autobahntypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

const evmOnlyPreparedTxCacheShards = 64
const evmOnlyPreparedTxCacheCapacity = 3 * autobahntypes.MaxLaneRangeInProposal * autobahntypes.MaxTxsPerBlock

type evmOnlyPreparedTxCache struct {
	shards [evmOnlyPreparedTxCacheShards]utils.RWMutex[*evmOnlyPreparedTxCacheShard]
}

type evmOnlyPreparedTxCacheShard struct {
	entries map[common.Hash]evmonly.PreparedTx
	order   []common.Hash
	next    int
}

func newEVMOnlyPreparedTxCache() *evmOnlyPreparedTxCache {
	cache := &evmOnlyPreparedTxCache{}
	shardCapacity := (int(evmOnlyPreparedTxCacheCapacity) + evmOnlyPreparedTxCacheShards - 1) /
		evmOnlyPreparedTxCacheShards
	for i := range cache.shards {
		cache.shards[i] = utils.NewRWMutex(&evmOnlyPreparedTxCacheShard{
			entries: make(map[common.Hash]evmonly.PreparedTx, shardCapacity),
			order:   make([]common.Hash, 0, shardCapacity),
		})
	}
	return cache
}

func (c *evmOnlyPreparedTxCache) Put(hash common.Hash, prepared evmonly.PreparedTx) {
	shard := &c.shards[int(hash[0])%len(c.shards)]
	for entries := range shard.Lock() {
		if _, ok := entries.entries[hash]; ok {
			entries.entries[hash] = prepared
			return
		}
		if len(entries.order) < cap(entries.order) {
			entries.order = append(entries.order, hash)
		} else {
			delete(entries.entries, entries.order[entries.next])
			entries.order[entries.next] = hash
			entries.next = (entries.next + 1) % cap(entries.order)
		}
		entries.entries[hash] = prepared
	}
}

func (c *evmOnlyPreparedTxCache) Lookup(hash common.Hash) (evmonly.PreparedTx, bool) {
	shard := &c.shards[int(hash[0])%len(c.shards)]
	for entries := range shard.RLock() {
		prepared, ok := entries.entries[hash]
		return prepared, ok
	}
	panic("unreachable")
}
