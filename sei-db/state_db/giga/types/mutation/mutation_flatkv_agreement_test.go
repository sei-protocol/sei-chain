package mutation_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// This test lives in an external package so that flatKV can import the mutation package without a cycle.

// TestMutationsAgreeWithFlatKV converts each block's changesets into mutations against a flatKV store's committed
// state, then requires that every mutation's new value is the row flatKV stores for its key, and that a second
// store fed the mutations converted back into changesets reaches the same lattice hash.
func TestMutationsAgreeWithFlatKV(t *testing.T) {
	source := newFlatKVStore(t)
	replica := newFlatKVStore(t)

	for i, block := range agreementBlocks() {
		height := uint64(i + 1)

		prior := source.OpenView()
		mutations, err := mutation.MutationsFromChangeSets(block, height, prior)
		prior.Close()
		require.NoError(t, err, "block %d", height)

		commitBlock(t, source, height, block)
		converted, err := mutation.MutationsToChangeSets(mutations)
		require.NoError(t, err, "block %d", height)
		commitBlock(t, replica, height, converted)

		view := source.OpenView()
		for _, m := range mutations {
			row, found := view.GetRaw(m.Key)
			require.Equal(t, m.NewValue != nil, found, "block %d key %x", height, m.Key)
			require.Equal(t, m.NewValue, row, "block %d key %x", height, m.Key)
		}
		view.Close()

		require.Equal(t, latestHash(t, source), latestHash(t, replica), "block %d", height)
	}
}

// agreementBlocks returns a sequence of blocks, each a list of changesets, that writes and deletes every kind of row
// and changes account fields one at a time.
func agreementBlocks() [][]*proto.NamedChangeSet {
	addressA := bytes.Repeat([]byte{0xa1}, keys.AddressLen)
	addressB := bytes.Repeat([]byte{0xb2}, keys.AddressLen)
	slot1 := append(bytes.Clone(addressA), bytes.Repeat([]byte{0x01}, 32)...)
	slot2 := append(bytes.Clone(addressA), bytes.Repeat([]byte{0x02}, 32)...)
	evmMiscKey := append([]byte{0x01}, addressA...)

	return [][]*proto.NamedChangeSet{
		{
			evmChangeSet(
				set(keys.BuildEVMKey(keys.EVMKeyNonce, addressA), nonce(1)),
				set(keys.BuildEVMKey(keys.EVMKeyCodeHash, addressA), filled(32, 0xc0)),
				set(keys.BuildEVMKey(keys.EVMKeyBalance, addressA), filled(32, 0x0a)),
				set(keys.BuildEVMKey(keys.EVMKeyBalance, addressB), filled(32, 0x0b)),
				set(keys.BuildEVMKey(keys.EVMKeyStorage, slot1), filled(32, 0x51)),
				set(keys.BuildEVMKey(keys.EVMKeyStorage, slot2), make([]byte, 32)),
				set(keys.BuildEVMKey(keys.EVMKeyCode, addressA), []byte{0x60, 0x80, 0x60, 0x40}),
				set(evmMiscKey, []byte("misc")),
			),
			moduleChangeSet("bank", set([]byte("supply"), []byte("100"))),
		},
		{
			evmChangeSet(
				set(keys.BuildEVMKey(keys.EVMKeyNonce, addressA), nonce(2)),
				del(keys.BuildEVMKey(keys.EVMKeyBalance, addressB)),
				set(keys.BuildEVMKey(keys.EVMKeyStorage, slot1), filled(32, 0x52)),
				set(keys.BuildEVMKey(keys.EVMKeyStorage, slot2), filled(32, 0x61)),
				del(keys.BuildEVMKey(keys.EVMKeyCode, addressA)),
				del(evmMiscKey),
			),
			moduleChangeSet("bank", set([]byte("supply"), nil)),
			evmChangeSet(set(keys.BuildEVMKey(keys.EVMKeyStorage, slot1), filled(32, 0x53))),
		},
		{
			evmChangeSet(
				set(keys.BuildEVMKey(keys.EVMKeyCodeHash, addressB), filled(32, 0xc1)),
				set(keys.BuildEVMKey(keys.EVMKeyStorage, slot1), make([]byte, 32)),
				del(keys.BuildEVMKey(keys.EVMKeyCodeHash, addressA)),
			),
			moduleChangeSet("bank", del([]byte("supply"))),
		},
	}
}

// newFlatKVStore opens an empty flatKV store with no state WAL.
func newFlatKVStore(t *testing.T) *flatkv.CommitStore {
	t.Helper()
	// Not t.Context(): it is cancelled before cleanups run, and a store closed after its context is cancelled
	// cannot drain its in-flight blocks.
	store, err := flatkv.NewCommitStore(context.Background(), flatkvconfig.DefaultTestConfig(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.LoadLatest())
	return store
}

// commitBlock applies changeSets to store as block height and commits it.
func commitBlock(t *testing.T, store *flatkv.CommitStore, height uint64, changeSets []*proto.NamedChangeSet) {
	t.Helper()
	require.NoError(t, store.ApplyChangeSets(int64(height), changeSets)) //nolint:gosec // test heights are small
	_, err := store.Commit(int64(height))                                //nolint:gosec // test heights are small
	require.NoError(t, err)
}

// latestHash returns the global lattice hash of the most recently committed block of store.
func latestHash(t *testing.T, store *flatkv.CommitStore) [32]byte {
	t.Helper()
	require.NoError(t, store.FlushHashes())
	hash, err := store.RegisterHashListener(func(context.Context, int64, *lthash.BlockHash) error { return nil })
	require.NoError(t, err)
	return hash.Global.Checksum()
}

func evmChangeSet(pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return moduleChangeSet(keys.EVMStoreKey, pairs...)
}

func moduleChangeSet(moduleName string, pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{Name: moduleName, Changeset: proto.ChangeSet{Pairs: pairs}}
}

func set(key []byte, value []byte) *proto.KVPair {
	return &proto.KVPair{Key: key, Value: value}
}

func del(key []byte) *proto.KVPair {
	return &proto.KVPair{Key: key, Delete: true}
}

func nonce(value uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, value)
}

func filled(length int, value byte) []byte {
	return bytes.Repeat([]byte{value}, length)
}
