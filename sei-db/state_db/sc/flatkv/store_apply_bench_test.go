package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
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

// benchClassified builds the classified buckets for a block made of the given per-kind counts,
// mirroring what classifyAndPrefix produces. Account writes use codehash keys, matching the
// cryptosim harness, which drives accounts through the codehash arm and never writes a nonce.
func benchClassified(b *testing.B, accounts int, storage int, code int, misc int, codeSize int) classifiedChanges {
	b.Helper()
	pairs := make([]*proto.KVPair, 0, accounts+storage+code+misc)
	for i := 0; i < accounts; i++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, benchAddr(i)),
			Value: benchSlot(i),
		})
	}
	for i := 0; i < storage; i++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   keys.BuildEVMKey(keys.EVMKeyStorage, append(benchAddr(i), benchSlot(i)...)),
			Value: benchSlot(i),
		})
	}
	for i := 0; i < code; i++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   keys.BuildEVMKey(keys.EVMKeyCode, benchAddr(i)),
			Value: bytes.Repeat([]byte{byte(i)}, codeSize),
		})
	}
	for i := 0; i < misc; i++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   append([]byte{0x1b}, benchAddr(i)...),
			Value: benchSlot(i),
		})
	}

	classified, err := classifyAndPrefix(fatChangeSets(pairs), [keys.EVMKeyKindCount]int{})
	if err != nil {
		b.Fatal(err)
	}
	return classified
}

// benchAccountOld returns the prior account values for the codehash bucket, so the merge exercises
// the path that copies an existing account rather than the one that creates a fresh one.
func benchAccountOld(b *testing.B, classified classifiedChanges) map[string]*vtype.AccountData {
	b.Helper()
	old := make(map[string]*vtype.AccountData, len(classified[keys.EVMKeyCodeHash]))
	for i, change := range classified[keys.EVMKeyCodeHash] {
		old[change.key] = vtype.NewAccountData().SetBlockHeight(1).SetNonce(uint64(i))
	}
	return old
}

// BenchmarkGatherValues covers the work in the apply_change_sets_gather_values phase: everything
// prepareWrites does after the account read. Each kind runs on its own so a change to one is not
// hidden by the others, and "cryptosim_mix" reproduces the harness's measured per-block shape.
func BenchmarkGatherValues(b *testing.B) {
	cases := []struct {
		name                                   string
		accounts, storage, code, misc, codeLen int
	}{
		{"accounts_only", 2000, 0, 0, 0, 0},
		{"storage_only", 0, 2000, 0, 0, 0},
		{"code_only", 0, 0, 2000, 0, 2048},
		{"misc_only", 0, 0, 0, 2000, 0},
		{"cryptosim_mix", 1930, 2030, 3, 0, 8},
	}
	for _, tc := range cases {
		classified := benchClassified(b, tc.accounts, tc.storage, tc.code, tc.misc, tc.codeLen)
		accountOld := benchAccountOld(b, classified)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := gatherValues(classified, accountOld, 100); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGatherAndSerialize covers gathering plus the Serialize call every value makes on its way
// into the stores. Holding a value's serialized form moves cost out of Serialize and into
// construction, so measuring either half alone misreports it; this measures both.
func BenchmarkGatherAndSerialize(b *testing.B) {
	cases := []struct {
		name                                   string
		accounts, storage, code, misc, codeLen int
	}{
		{"accounts_only", 2000, 0, 0, 0, 0},
		{"storage_only", 0, 2000, 0, 0, 0},
		{"code_only", 0, 0, 2000, 0, 2048},
		{"misc_only", 0, 0, 0, 2000, 0},
		{"cryptosim_mix", 1930, 2030, 3, 0, 8},
	}
	for _, tc := range cases {
		classified := benchClassified(b, tc.accounts, tc.storage, tc.code, tc.misc, tc.codeLen)
		accountOld := benchAccountOld(b, classified)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				prepared, err := gatherValues(classified, accountOld, 100)
				if err != nil {
					b.Fatal(err)
				}
				benchSerializeAll(prepared)
			}
		})
	}
}

// benchSerializeAll performs the same per-value work serializeAndPut does, minus the store write.
func benchSerializeAll(prepared preparedWrites) {
	for _, value := range prepared.accounts {
		sink(value.IsDelete(), value.Serialize())
	}
	for _, value := range prepared.storage {
		sink(value.IsDelete(), value.Serialize())
	}
	for _, value := range prepared.code {
		sink(value.IsDelete(), value.Serialize())
	}
	for _, value := range prepared.misc {
		sink(value.IsDelete(), value.Serialize())
	}
}

// benchSink keeps serialized bytes from being optimized away.
var benchSink []byte

func sink(isDelete bool, serialized []byte) {
	if !isDelete {
		benchSink = serialized
	}
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
