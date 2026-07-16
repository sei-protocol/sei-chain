package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestPruneZeroStorageSlots(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)

	addr1 := common.HexToAddress("0x0100000000000000000000000000000000000000")
	addr2 := common.HexToAddress("0x0200000000000000000000000000000000000000")

	zero := common.Hash{}
	nonZero := common.HexToHash("0x1234")
	slot1 := common.HexToHash("0x01")
	slot2 := common.HexToHash("0x02")
	slot3 := common.HexToHash("0x03")

	store1 := k.PrefixStore(ctx, types.StateKey(addr1))
	store1.Set(slot1[:], zero[:])
	store1.Set(slot2[:], nonZero[:])

	store2 := k.PrefixStore(ctx, types.StateKey(addr2))
	store2.Set(slot3[:], zero[:])

	processed, deleted := k.PruneZeroStorageSlots(ctx, 2)
	require.Equal(t, 2, processed)
	require.Equal(t, 1, deleted)

	store1 = k.PrefixStore(ctx, types.StateKey(addr1))
	require.False(t, store1.Has(slot1[:]))
	require.True(t, store1.Has(slot2[:]))

	expectedCheckpoint := append(addr1.Bytes(), slot2.Bytes()...)
	require.Equal(t, expectedCheckpoint, k.GetZeroStorageCleanupCheckpoint(ctx))

	processed, deleted = k.PruneZeroStorageSlots(ctx, 2)
	require.Equal(t, 1, processed)
	require.Equal(t, 1, deleted)

	store2 = k.PrefixStore(ctx, types.StateKey(addr2))
	require.False(t, store2.Has(slot3[:]))
	require.Nil(t, k.GetZeroStorageCleanupCheckpoint(ctx))

	processed, deleted = k.PruneZeroStorageSlots(ctx, evmkeeper.ZeroStorageCleanupBatchSize)
	require.Equal(t, 1, processed)
	require.Equal(t, 0, deleted)
}

// TestPruneZeroStorageSlots_DecidesDeletionFromRoutedGet guards the
// migration-safety contract: the prune decision must come from the routed
// logical read (store.Get), not from the iterator's surfaced value. During a
// flatkv migration the iterator is a merged view across backends and can
// surface a value that disagrees with the authoritative routed read. We
// simulate that divergence with a store whose iterator lies about Value()
// while Get() returns the truth.
//
// Without the fix (val := iterator.Value()) this test fails: the live slot is
// wrongly pruned and the dead slot is wrongly kept.
func TestPruneZeroStorageSlots_DecidesDeletionFromRoutedGet(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)

	// liveAddr/liveSlot: authoritative Get is non-zero, but the iterator
	// surfaces it as zero. It must survive (deleting it would lose live state).
	liveAddr := common.HexToAddress("0x0100000000000000000000000000000000000000")
	liveSlot := common.HexToHash("0x01")
	// deadAddr/deadSlot: authoritative Get is zero, but the iterator surfaces
	// it as non-zero. It must be pruned (decision follows Get).
	deadAddr := common.HexToAddress("0x0200000000000000000000000000000000000000")
	deadSlot := common.HexToHash("0x02")

	zero := common.Hash{}
	nonZero := common.HexToHash("0x1234")

	liveStore := k.PrefixStore(ctx, types.StateKey(liveAddr))
	liveStore.Set(liveSlot[:], nonZero[:])
	deadStore := k.PrefixStore(ctx, types.StateKey(deadAddr))
	deadStore.Set(deadSlot[:], zero[:])

	liveKey := evmStateFullKey(liveAddr, liveSlot)
	deadKey := evmStateFullKey(deadAddr, deadSlot)
	iteratorLies := map[string][]byte{
		string(liveKey): zero[:],    // iterator pretends the live slot is zero
		string(deadKey): nonZero[:], // iterator pretends the dead slot is non-zero
	}

	storeKey := k.GetStoreKey()
	lying := iteratorValueLyingStore{
		KVStore: ctx.MultiStore().GetKVStore(storeKey),
		lies:    iteratorLies,
	}
	ctx = ctx.WithMultiStore(kvStoreOverrideMultiStore{
		MultiStore: ctx.MultiStore(),
		target:     storeKey,
		store:      lying,
	})

	processed, deleted := k.PruneZeroStorageSlots(ctx, evmkeeper.ZeroStorageCleanupBatchSize)
	require.Equal(t, 2, processed)
	require.Equal(t, 1, deleted)

	liveStore = k.PrefixStore(ctx, types.StateKey(liveAddr))
	require.True(t, liveStore.Has(liveSlot[:]), "live slot (routed Get non-zero) must survive despite iterator showing zero")
	deadStore = k.PrefixStore(ctx, types.StateKey(deadAddr))
	require.False(t, deadStore.Has(deadSlot[:]), "dead slot (routed Get zero) must be pruned despite iterator showing non-zero")
}

func evmStateFullKey(addr common.Address, slot common.Hash) []byte {
	out := make([]byte, 0, len(types.StateKeyPrefix)+len(addr)+len(slot))
	out = append(out, types.StateKeyPrefix...)
	out = append(out, addr.Bytes()...)
	out = append(out, slot[:]...)
	return out
}

// kvStoreOverrideMultiStore returns a custom KVStore for a single target store
// key and delegates everything else to the embedded MultiStore.
type kvStoreOverrideMultiStore struct {
	sdk.MultiStore
	target sdk.StoreKey
	store  sdk.KVStore
}

func (m kvStoreOverrideMultiStore) GetKVStore(key sdk.StoreKey) sdk.KVStore {
	if key.Name() == m.target.Name() {
		return m.store
	}
	return m.MultiStore.GetKVStore(key)
}

// iteratorValueLyingStore behaves like its underlying KVStore for point reads
// (Get/Has) but returns iterator values that may disagree with the routed read,
// mimicking a merged migration iterator.
type iteratorValueLyingStore struct {
	sdk.KVStore
	lies map[string][]byte
}

func (s iteratorValueLyingStore) Iterator(start, end []byte) sdk.Iterator {
	return lyingIterator{Iterator: s.KVStore.Iterator(start, end), lies: s.lies}
}

func (s iteratorValueLyingStore) ReverseIterator(start, end []byte) sdk.Iterator {
	return lyingIterator{Iterator: s.KVStore.ReverseIterator(start, end), lies: s.lies}
}

type lyingIterator struct {
	sdk.Iterator
	lies map[string][]byte
}

func (it lyingIterator) Value() []byte {
	if v, ok := it.lies[string(it.Key())]; ok {
		return v
	}
	return it.Iterator.Value()
}
