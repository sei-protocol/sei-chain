package flatkv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
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

// benchPriorValues returns the row each account already holds, as the account store would hand it to
// an accountUpdater: every account the codehash bucket names already carries a prior value, so the
// merge exercises the path that folds onto an existing row rather than the one that starts at zero.
func benchPriorValues(b *testing.B, classified classifiedChanges) map[string][]byte {
	b.Helper()
	prior := make(map[string][]byte, len(classified[keys.EVMKeyCodeHash]))
	for i, change := range classified[keys.EVMKeyCodeHash] {
		prior[change.key] = vtype.NewAccountData().SetBlockHeight(1).SetNonce(uint64(i)).Serialize()
	}
	return prior
}

// benchPrepare runs the value-building work of an apply: the three databases gathered off the apply
// thread, plus folding every account change onto the row that account already holds. The fold happens
// inside the account store's write in production, so prior stands in for what the store supplies.
func benchPrepare(classified classifiedChanges, prior map[string][]byte) (preparedWrites, error) {
	out, err := gatherNonAccountValues(classified, 100)
	if err != nil {
		return preparedWrites{}, err
	}
	updater, err := newAccountUpdater(
		classified[keys.EVMKeyNonce],
		classified[keys.EVMKeyCodeHash],
		nil,
		100,
	)
	if err != nil {
		return preparedWrites{}, err
	}
	if updater != nil {
		for _, key := range updater.keys {
			value, err := updater.NewValueFor(key, prior[key])
			if err != nil {
				return preparedWrites{}, err
			}
			sink(value == nil, value)
		}
	}
	out.accounts = updater
	return out, nil
}

// benchAccountPairs returns n codehash writes, one per account, matching the shape the cryptosim
// harness produces: it drives every account through the codehash arm and never writes a nonce.
// Code hashes start at 1, since an account whose every field is zero stores as a deletion and would
// not come back from a read.
func benchAccountPairs(n int) []*proto.KVPair {
	pairs := make([]*proto.KVPair, 0, n)
	for i := 0; i < n; i++ {
		pairs = append(pairs, &proto.KVPair{
			Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, benchAddr(i)),
			Value: benchSlot(i + 1),
		})
	}
	return pairs
}

// benchWarmStore returns a store where every one of pairs' accounts has been written, committed and
// flushed. That is the state a running node's apply path reads against — the accounts a block touches
// were written by earlier blocks, so they are served from the read cache rather than from the block's
// own uncommitted writes.
func benchWarmStore(b *testing.B, pairs []*proto.KVPair) *CommitStore {
	b.Helper()
	s, err := newCommitStoreWithWAL(b.Context(), config.DefaultTestConfig(b))
	if err != nil {
		b.Fatal(err)
	}
	if err := s.LoadLatest(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	})
	if err := s.ApplyChangeSets(1, fatChangeSets(pairs)); err != nil {
		b.Fatal(err)
	}
	if _, err := s.Commit(1); err != nil {
		b.Fatal(err)
	}
	if err := s.FlushHashes(); err != nil {
		b.Fatal(err)
	}
	if err := s.flushLatestVersion(); err != nil {
		b.Fatal(err)
	}
	return s
}

// BenchmarkAccountUpdate covers what replaced the apply_change_sets_read_accounts phase: folding a
// block's account changes onto the rows they modify, inside the account store's write, with every key
// already in cache. Sizes span one block's worth of account writes at the 2000-transaction consensus
// cap and at the doubled block the rf-perf scenario drives.
func BenchmarkAccountUpdate(b *testing.B) {
	for _, accounts := range []int{2000, 8000} {
		b.Run(fmt.Sprintf("accounts=%d", accounts), func(b *testing.B) {
			pairs := benchAccountPairs(accounts)
			s := benchWarmStore(b, pairs)
			classified, err := classifyAndPrefix(fatChangeSets(pairs), [keys.EVMKeyKindCount]int{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				updater, err := newAccountUpdater(
					classified[keys.EVMKeyNonce],
					classified[keys.EVMKeyCodeHash],
					nil,
					100,
				)
				if err != nil {
					b.Fatal(err)
				}
				if len(updater.keys) != accounts {
					b.Fatalf("updating %d accounts, want %d", len(updater.keys), accounts)
				}
				if err := s.accountStore.BatchUpdate(updater.keys, updater); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGatherValues covers everything prepareWrites does other than the account read: the three
// databases gathered off the apply thread plus the account merge on it. Each kind runs on its own so
// a change to one is not hidden by the others, and "cryptosim_mix" reproduces the harness's measured
// per-block shape.
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
		prior := benchPriorValues(b, classified)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := benchPrepare(classified, prior); err != nil {
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
		prior := benchPriorValues(b, classified)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				prepared, err := benchPrepare(classified, prior)
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
