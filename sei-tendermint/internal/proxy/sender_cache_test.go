package proxy

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestSenderCacheKeepsRecentEntriesAcrossRollover(t *testing.T) {
	hashOf := func(i int) common.Hash { return common.BigToHash(big.NewInt(int64(i))) }
	addrOf := func(i int) common.Address { return common.BigToAddress(big.NewInt(int64(i))) }
	const capacity = 4
	cache := newSenderCache(capacity)
	for i := range 3*capacity + 1 {
		cache.put(hashOf(i), addrOf(i))
	}

	require.Equal(t, utils.None[common.Address](), cache.take(hashOf(capacity)))
	require.Equal(t, utils.Some(addrOf(2*capacity)), cache.take(hashOf(2*capacity)))
	require.Equal(t, utils.Some(addrOf(3*capacity)), cache.take(hashOf(3*capacity)))
	require.Equal(t, utils.None[common.Address](), cache.take(hashOf(2*capacity)))
}
