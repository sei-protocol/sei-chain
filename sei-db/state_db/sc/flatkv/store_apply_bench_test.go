package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// benchAddr returns the i'th deterministic 20-byte address.
func benchAddr(i int) []byte {
	addr := make([]byte, keys.AddressLen)
	binary.BigEndian.PutUint64(addr[keys.AddressLen-8:], uint64(i))
	return addr
}

// benchSlot returns the i'th deterministic 32-byte storage slot.
func benchSlot(i int) []byte {
	slot := make([]byte, 32)
	binary.BigEndian.PutUint64(slot[24:], uint64(i))
	return slot
}

// benchPairs builds n changeset pairs in roughly the proportion the ERC20 benchmark scenario
// produces: one nonce write per transaction and two storage writes, plus the handful of per-block
// misc keys. Keys are returned in ascending raw-key order, matching what a production block hands
// down (see the sorted cachekv flush in sei-cosmos).
func benchPairs(n int) []*proto.KVPair {
	pairs := make([]*proto.KVPair, 0, n+2)
	for i := 0; i < n; i++ {
		if i%3 == 0 {
			pairs = append(pairs, &proto.KVPair{
				Key:   keys.BuildEVMKey(keys.EVMKeyNonce, benchAddr(i)),
				Value: binary.BigEndian.AppendUint64(nil, uint64(i)),
			})
			continue
		}
		slotKey := append(benchAddr(i), benchSlot(i)...)
		pairs = append(pairs, &proto.KVPair{
			Key:   keys.BuildEVMKey(keys.EVMKeyStorage, slotKey),
			Value: benchSlot(i),
		})
	}
	// Per-block misc keys: base fee and next base fee.
	pairs = append(pairs,
		&proto.KVPair{Key: []byte{0x1b}, Value: benchSlot(1)},
		&proto.KVPair{Key: []byte{0x1c}, Value: benchSlot(2)},
	)
	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0
	})
	return pairs
}

// fatChangeSets wraps every pair in a single NamedChangeSet, the shape a production block produces:
// rootmulti emits one changeset per module and the evm one carries all of that module's pairs.
func fatChangeSets(pairs []*proto.KVPair) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: pairs},
	}}
}

// singlePairChangeSets wraps each pair in its own NamedChangeSet, the shape the cryptosim harness
// produces. Kept alongside fatChangeSets so the divergence between the two stays measurable.
func singlePairChangeSets(pairs []*proto.KVPair) []*proto.NamedChangeSet {
	out := make([]*proto.NamedChangeSet, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, &proto.NamedChangeSet{
			Name:      keys.EVMStoreKey,
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{pair}},
		})
	}
	return out
}

func BenchmarkClassifyAndPrefix(b *testing.B) {
	shapes := []struct {
		name  string
		build func([]*proto.KVPair) []*proto.NamedChangeSet
	}{
		{"fat_changeset", fatChangeSets},
		{"single_pair_changesets", singlePairChangeSets},
	}
	for _, size := range []int{1000, 3000, 5000} {
		for _, shape := range shapes {
			changeSets := shape.build(benchPairs(size))
			b.Run(fmt.Sprintf("%s/pairs=%d", shape.name, size), func(b *testing.B) {
				b.ReportAllocs()
				// Carried across iterations exactly as the store carries it across blocks.
				var sizeHints [keys.EVMKeyKindCount]int
				for b.Loop() {
					classified, err := classifyAndPrefix(changeSets, sizeHints)
					if err != nil {
						b.Fatal(err)
					}
					sizeHints = classified.bucketSizes()
				}
			})
		}
	}
}
