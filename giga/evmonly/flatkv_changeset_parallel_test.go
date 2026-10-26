package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// largeFlatKVChangeSet returns n changes of each kind, plus some code changes, with every encoding
// branch represented.
func largeFlatKVChangeSet(n int) StateChangeSet {
	addr := func(kind byte, i int) common.Address {
		return common.Address{kind, byte(i >> 16), byte(i >> 8), byte(i)}
	}
	var changes StateChangeSet
	for i := range n {
		balance := big.NewInt(int64(i))
		if i%7 == 0 {
			balance = nil
		}
		changes.Balances = append(changes.Balances, BalanceChange{Address: addr(1, i), Balance: balance})
		changes.Nonces = append(changes.Nonces, NonceChange{Address: addr(2, i), Nonce: uint64(i)})
		changes.Storage = append(changes.Storage, StorageChange{
			Address: addr(3, i),
			Key:     common.BigToHash(big.NewInt(int64(i))),
			Value:   common.BigToHash(big.NewInt(int64(i) + 1)),
			Delete:  i%5 == 0,
		})
	}
	for i := range n / 16 {
		changes.Code = append(changes.Code, CodeChange{Address: addr(4, i), Code: []byte{0x60, byte(i)}, Delete: i%3 == 0})
	}
	return changes
}

func flatKVBuilderPairs(b *flatKVChangeSetBuilder) []proto.KVPair {
	pairs := make([]proto.KVPair, len(b.pairPtrs))
	for i, pair := range b.pairPtrs {
		pairs[i] = *pair
	}
	return pairs
}

// The chunked parallel fill feeds FlatKV's LtHash, so it has to lay out exactly the pairs one serial
// pass does.
func TestFlatKVParallelFillMatchesASerialPass(t *testing.T) {
	changes := largeFlatKVChangeSet(3000)
	total := flatKVChangeCount(changes)
	require.GreaterOrEqual(t, total, 2*flatKVParallelEncodeChunk, "the change set must take the parallel path")

	parallel := newFlatKVChangeSetBuilder(changes)
	parallel.fill(changes)
	serial := newFlatKVChangeSetBuilder(changes)
	serial.encodeRange(changes, 0, total)

	require.Equal(t, flatKVBuilderPairs(serial), flatKVBuilderPairs(parallel))
}
