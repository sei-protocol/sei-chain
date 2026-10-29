package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// largeFlatKVChangeSet returns n account changes and n storage changes with every encoding branch
// represented: balances set and deleted, nonces, code set and deleted, and storage set and deleted.
func largeFlatKVChangeSet(n int) StateChangeSet {
	addr := func(kind byte, i int) common.Address {
		return common.Address{kind, byte(i >> 16), byte(i >> 8), byte(i)}
	}
	var changes StateChangeSet
	for i := range n {
		account := AccountChange{Address: addr(1, i), Prior: AccountState{Balance: big.NewInt(1), Nonce: 1}}
		account.Account = account.Prior
		switch i % 4 {
		case 0:
			account.Account.Balance = big.NewInt(int64(i) + 2)
		case 1:
			account.Account.Balance = nil
			account.Account.Nonce = uint64(i) + 2
		case 2:
			account.Account.Nonce = uint64(i) + 2
			account.Account.Code = []byte{0x60, byte(i)}
		default:
			account.Prior.Code = []byte{0x60}
			account.Account.Balance = big.NewInt(int64(i) + 2)
		}
		changes.Accounts = append(changes.Accounts, account)
		changes.Storage = append(changes.Storage, StorageChange{
			Address: addr(3, i),
			Key:     common.BigToHash(big.NewInt(int64(i))),
			Value:   common.BigToHash(big.NewInt(int64(i) + 1)),
			Delete:  i%5 == 0,
		})
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
	changes := largeFlatKVChangeSet(5000)
	total := len(changes.Accounts) + len(changes.Storage)
	require.GreaterOrEqual(t, total, 2*flatKVParallelEncodeChunk, "the change set must take the parallel path")

	parallel, err := newFlatKVChangeSetBuilder(changes)
	require.NoError(t, err)
	parallel.fill(changes)
	serial, err := newFlatKVChangeSetBuilder(changes)
	require.NoError(t, err)
	serial.encodeRange(changes, 0, total)

	require.Equal(t, flatKVBuilderPairs(serial), flatKVBuilderPairs(parallel))
}
