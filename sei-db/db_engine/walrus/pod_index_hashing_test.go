package walrus

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// evmShapedKey builds a key with the layout real storage keys have: a type byte, a contract address, and a
// slot. Every key of one contract shares its leading bytes, which is the case an ordering on a key prefix
// collapses into a single run.
func evmShapedKey(contract byte, slot uint64) []byte {
	key := make([]byte, 0, 53)
	key = append(key, 0x03)
	for index := 0; index < 20; index++ {
		key = append(key, contract)
	}
	key = append(key, make([]byte, 24)...)
	return binary.BigEndian.AppendUint64(key, slot)
}

// findCollidingKeys returns two distinct keys whose index hashes are equal under salt.
//
// A collision is what the index's disambiguation path exists for, and at one in four billion it would never
// be reached by keys chosen any other way.
func findCollidingKeys(t *testing.T, salt uint64) (first []byte, second []byte) {
	t.Helper()

	seen := map[uint32][]byte{}
	for index := 0; index < 1<<22; index++ {
		key := []byte(fmt.Sprintf("collision-probe-%08d", index))
		hash := indexHash(podKeyHash(salt, key))
		if previous, ok := seen[hash]; ok {
			return previous, key
		}
		seen[hash] = key
	}
	t.Fatalf("no index hash collision found under salt %d", salt)
	return nil, nil
}

// TestPodIndexResolvesAHashCollision checks that two keys sharing an index hash are told apart.
//
// The hash index cannot distinguish them, so the whole of the search's correctness at that point rests on
// comparing the key held in each of their records.
func TestPodIndexResolvesAHashCollision(t *testing.T) {
	const salt = uint64(0x1234_5678_9abc_def0)
	first, second := findCollidingKeys(t, salt)
	require.Equal(t, indexHash(podKeyHash(salt, first)), indexHash(podKeyHash(salt, second)))

	directory := t.TempDir()
	refs := []podEntryRef{
		{hash: podKeyHash(salt, first), key: first, blockDelta: 0, offset: 111},
		{hash: podKeyHash(salt, second), key: second, blockDelta: 1, offset: 222},
	}
	_, _, _, err := writePodIndex(directory, refs, 10, 11, salt)
	require.NoError(t, err)

	index, err := openPodIndex(directory)
	require.NoError(t, err)
	require.Equal(t, uint64(2), index.hashes.keyCount, "the collision must occupy two entries, not one")

	// Each key resolves to its own entry rather than to whichever one the search landed on.
	offset, block, found, present, err := index.FindNewest(first, 0, 100)
	require.NoError(t, err)
	require.True(t, found && present)
	require.Equal(t, uint32(111), offset)
	require.Equal(t, uint64(10), block)

	offset, block, found, present, err = index.FindNewest(second, 0, 100)
	require.NoError(t, err)
	require.True(t, found && present)
	require.Equal(t, uint32(222), offset)
	require.Equal(t, uint64(11), block)

	// A key that hashes elsewhere is absent, and so is one that would land in the collided run if the run
	// were walked without comparing keys.
	_, _, found, present, err = index.FindNewest([]byte("neither-of-them"), 0, 100)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, present)
}

// TestPodIndexAnswersKeysSharingALongPrefix checks a pod whose keys all share their leading bytes.
//
// Under an ordering on a key prefix every one of these keys occupies the same run, so a search for any of
// them walks the whole contract. The hashing is what makes them scatter, and the answers have to survive it.
func TestPodIndexAnswersKeysSharingALongPrefix(t *testing.T) {
	const slotCount = 2_000

	pairs := make([]*proto.KVPair, 0, slotCount)
	for slot := uint64(0); slot < slotCount; slot++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   evmShapedKey(0xab, slot),
			Value: binary.BigEndian.AppendUint64(nil, slot),
		})
	}

	directory := t.TempDir()
	pod, err := newPodBuilder(directory, DefaultConfig(directory, "test", "evm")).
		Build([]Block{{Number: 7, ChangeSets: []*proto.NamedChangeSet{
			{Name: "evm", Changeset: proto.ChangeSet{Pairs: pairs}},
		}}})
	require.NoError(t, err)

	for slot := uint64(0); slot < slotCount; slot++ {
		key := evmShapedKey(0xab, slot)
		offset, block, found, _, err := pod.Index.FindNewest(key, 0, 100)
		require.NoError(t, err)
		require.True(t, found, "slot %d is missing from the index", slot)
		require.Equal(t, uint64(7), block)

		value, _, err := pod.Data.ReadEntry(offset)
		require.NoError(t, err)
		require.Equal(t, binary.BigEndian.AppendUint64(nil, slot), value,
			"slot %d resolved to another key", slot)
	}

	// A slot of the same contract that was never written must not be found, however close its key is to the
	// ones that were.
	_, _, found, present, err := pod.Index.FindNewest(evmShapedKey(0xab, slotCount), 0, 100)
	require.NoError(t, err)
	require.False(t, found)
	require.False(t, present)
}

// TestSortEntryRefsOrdersKeysSharingALongPrefix exercises the parallel sort with keys that share their
// leading bytes.
//
// The parallel path buckets on the high bits of the hash. Bucketing on the high bits of a key would put every
// key of one contract in one bucket, and this input is that shape.
func TestSortEntryRefsOrdersKeysSharingALongPrefix(t *testing.T) {
	const salt = uint64(0x0f0f_0f0f_0f0f_0f0f)
	const refCount = 1 << 17 // Above the threshold where sortEntryRefs goes parallel.

	refs := make([]podEntryRef, 0, refCount)
	for index := 0; index < refCount; index++ {
		key := evmShapedKey(0xcd, uint64(index))
		refs = append(refs, podEntryRef{
			hash:       podKeyHash(salt, key),
			key:        key,
			blockDelta: uint32(index % 8),
			offset:     uint32(index),
		})
	}

	sortEntryRefs(refs)
	require.Len(t, refs, refCount, "the parallel sort dropped references")
	for index := 1; index < len(refs); index++ {
		require.False(t, lessEntryRef(refs[index], refs[index-1]),
			"references are out of order at %d", index)
	}

	// Every reference has to survive, not merely be ordered: the buckets are concatenated rather than merged.
	offsets := make(map[uint32]bool, refCount)
	for _, ref := range refs {
		offsets[ref.offset] = true
	}
	require.Len(t, offsets, refCount, "the parallel sort lost or duplicated references")
}
