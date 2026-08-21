package evmonly

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigastore "github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
)

func TestEncodeMemoryStoreChangeSetUsesOwnedDirectPairs(t *testing.T) {
	address := testAddress(0xd1)
	slot := common.HexToHash("0x1234")
	storageValue := common.HexToHash("0x5678")
	code := []byte{0x60, 0x01}
	balance := big.NewInt(42)
	changesets, err := EncodeMemoryStoreChangeSet(StateChangeSet{
		Balances:      []BalanceChange{{Address: address, Balance: balance}},
		Nonces:        []NonceChange{{Address: address, Nonce: 9}},
		Code:          []CodeChange{{Address: address, Code: code}},
		StorageClears: []common.Address{address},
		Storage: []StorageChange{{
			Address: address,
			Key:     slot,
			Value:   storageValue,
		}},
	})
	require.NoError(t, err)
	require.Len(t, changesets, 1)
	require.Equal(t, MemoryStoreChangeSetName, changesets[0].Name)
	pairs := changesets[0].Changeset.Pairs
	require.Len(t, pairs, 5)

	require.Equal(t, memoryStoreBalanceKey, pairs[0].Key[0])
	require.Equal(t, address[:], pairs[0].Key[1:])
	require.Equal(t, big.NewInt(42), new(big.Int).SetBytes(pairs[0].Value))
	require.Equal(t, memoryStoreNonceKey, pairs[1].Key[0])
	require.Equal(t, uint64(9), binary.BigEndian.Uint64(pairs[1].Value))
	require.Equal(t, memoryStoreCodeKey, pairs[2].Key[0])
	require.Equal(t, []byte{0x60, 0x01}, pairs[2].Value)
	require.Equal(t, memoryStoreStorageClearKey, pairs[3].Key[0])
	require.Empty(t, pairs[3].Value)
	require.Equal(t, memoryStoreStorageKeyKind, pairs[4].Key[0])
	require.Equal(t, slot[:], pairs[4].Key[memoryStoreAccountKeyLen:])
	require.Equal(t, storageValue[:], pairs[4].Value)

	code[0] = 0xff
	balance.SetInt64(99)
	require.Equal(t, []byte{0x60, 0x01}, pairs[2].Value)
	require.Equal(t, big.NewInt(42), new(big.Int).SetBytes(pairs[0].Value))

	store := NewMemoryStore(NewMemoryState())
	require.NoError(t, store.CommitStateChanges(1, changesets))
	snapshot := store.OpenSnapshot()
	defer snapshot.Close()
	for _, pair := range pairs {
		value, ok := snapshot.Get(pair.Key)
		require.True(t, ok)
		if pair.Key[0] == memoryStoreStorageClearKey {
			require.Empty(t, value)
			continue
		}
		require.Equal(t, pair.Value, value)
	}
}

func TestMemoryStoreSnapshotsRemainVersionedAcrossCommits(t *testing.T) {
	address := testAddress(0xe1)
	baseSlot := common.HexToHash("0x01")
	newSlot := common.HexToHash("0x02")
	base := NewMemoryState()
	base.SetBalance(address, big.NewInt(10))
	base.SetNonce(address, 1)
	base.SetCode(address, []byte{0x60, 0x00})
	base.SetState(address, baseSlot, common.HexToHash("0xaa"))
	store := NewMemoryStore(base)

	initial := store.OpenSnapshot()
	changesets, err := store.EncodeChangeSet(StateChangeSet{
		Balances: []BalanceChange{{Address: address, Balance: big.NewInt(20)}},
		Nonces:   []NonceChange{{Address: address, Nonce: 2}},
		Code:     []CodeChange{{Address: address, Code: []byte{0x60, 0x01}}},
		StorageClears: []common.Address{
			address,
		},
		Storage: []StorageChange{{
			Address: address,
			Key:     newSlot,
			Value:   common.HexToHash("0xbb"),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(7, changesets))

	current := store.OpenSnapshot()
	historical, ok := store.OpenSnapshotAt(7)
	require.True(t, ok)
	_, ok = store.OpenSnapshotAt(6)
	require.False(t, ok)

	require.Equal(t, int64(0), initial.GetBlockHeight())
	require.Equal(t, big.NewInt(10), gigaHashToBig(initial.GetBalance(gigastore.Address(address))))
	require.Equal(t, uint64(1), initial.GetNonce(gigastore.Address(address)))
	require.Equal(t, common.HexToHash("0xaa"), common.Hash(initial.GetStorage(gigastore.Address(address), gigastore.Hash(baseSlot))))

	for _, snapshot := range []gigastore.StateSnapshot{current, historical} {
		require.Equal(t, int64(7), snapshot.GetBlockHeight())
		require.Equal(t, big.NewInt(20), gigaHashToBig(snapshot.GetBalance(gigastore.Address(address))))
		require.Equal(t, uint64(2), snapshot.GetNonce(gigastore.Address(address)))
		require.Equal(t, []byte{0x60, 0x01}, snapshot.GetCode(gigastore.Address(address)))
		require.Equal(t, gigastore.Hash{}, snapshot.GetStorage(gigastore.Address(address), gigastore.Hash(baseSlot)))
		require.Equal(t, common.HexToHash("0xbb"), common.Hash(snapshot.GetStorage(gigastore.Address(address), gigastore.Hash(newSlot))))
	}

	deleteChanges, err := store.EncodeChangeSet(StateChangeSet{
		Code: []CodeChange{{Address: address, Delete: true}},
		Storage: []StorageChange{{
			Address: address,
			Key:     newSlot,
			Delete:  true,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(8, deleteChanges))
	afterDelete := store.OpenSnapshot()
	require.Empty(t, afterDelete.GetCode(gigastore.Address(address)))
	require.Equal(t, gigastore.Hash{}, afterDelete.GetStorage(gigastore.Address(address), gigastore.Hash(newSlot)))
	require.Equal(t, []byte{0x60, 0x01}, historical.GetCode(gigastore.Address(address)))
	require.Equal(t, common.HexToHash("0xbb"), common.Hash(historical.GetStorage(gigastore.Address(address), gigastore.Hash(newSlot))))

	initial.Close()
	current.Close()
	historical.Close()
	afterDelete.Close()
}

func TestMemoryStoreRejectsInvalidCommits(t *testing.T) {
	store := NewMemoryStore(NewMemoryState())
	changesets, err := store.EncodeChangeSet(StateChangeSet{})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(1, changesets))
	require.ErrorContains(t, store.CommitStateChanges(1, changesets), "not after current height")
	require.ErrorContains(t, store.CommitStateChanges(-1, changesets), "non-negative")
	require.ErrorContains(t, store.CommitStateChanges(2, []*proto.NamedChangeSet{{Name: "other"}}), "unsupported name")
	require.ErrorContains(t, store.CommitStateChanges(2, []*proto.NamedChangeSet{{
		Name:      MemoryStoreChangeSetName,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{nil}},
	}}), "pair 0 is nil")
	require.ErrorContains(t, store.CommitStateChanges(2, []*proto.NamedChangeSet{{
		Name: MemoryStoreChangeSetName,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key: []byte{memoryStoreBalanceKey},
		}}},
	}}), "balance key length")

	overflow := new(big.Int).Lsh(big.NewInt(1), 256)
	_, err = store.EncodeChangeSet(StateChangeSet{Balances: []BalanceChange{{Balance: overflow}}})
	require.ErrorContains(t, err, "unsigned 256-bit")
}

func TestMemoryStoreTracksZeroValueAndStorageOnlyAccounts(t *testing.T) {
	zeroValueAddress := testAddress(0xd2)
	storageOnlyAddress := testAddress(0xd3)
	store := NewMemoryStore(NewMemoryState())
	before := store.OpenSnapshot()

	changesets, err := store.EncodeChangeSet(StateChangeSet{
		Balances: []BalanceChange{{Address: zeroValueAddress, Balance: new(big.Int)}},
		Storage: []StorageChange{{
			Address: storageOnlyAddress,
			Key:     common.HexToHash("0x01"),
			Value:   common.HexToHash("0x02"),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, store.CommitStateChanges(5, changesets))
	after := store.OpenSnapshot()
	defer before.Close()
	defer after.Close()

	require.False(t, before.AccountExists(gigastore.Address(zeroValueAddress)))
	require.False(t, before.AccountExists(gigastore.Address(storageOnlyAddress)))
	require.True(t, after.AccountExists(gigastore.Address(zeroValueAddress)))
	require.True(t, after.AccountExists(gigastore.Address(storageOnlyAddress)))
}

func TestExecutorCommitsConsecutiveBlocksThroughMemoryStore(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xe2)
	base := NewMemoryState()
	base.SetBalance(sender, big.NewInt(testFundedBalanceWei))
	store := NewMemoryStore(base)
	executor := NewExecutor(Config{}, WithStore(store, store.EncodeChangeSet))

	for nonce := uint64(0); nonce < 2; nonce++ {
		ctx := blockContext(chainID)
		ctx.Number = nonce + 1
		rawTx := signLegacyTx(t, key, chainID, nonce, &recipient, big.NewInt(1), nil)
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
			Context: ctx,
			Txs:     [][]byte{rawTx},
		})
		require.NoError(t, err)
		result.Release()
	}

	snapshot := store.OpenSnapshot()
	defer snapshot.Close()
	require.Equal(t, int64(2), snapshot.GetBlockHeight())
	require.Equal(t, uint64(2), snapshot.GetNonce(gigastore.Address(sender)))
	require.Equal(t, big.NewInt(2), gigaHashToBig(snapshot.GetBalance(gigastore.Address(recipient))))
}

func gigaHashToBig(value gigastore.Hash) *big.Int {
	return new(big.Int).SetBytes(value[:])
}
