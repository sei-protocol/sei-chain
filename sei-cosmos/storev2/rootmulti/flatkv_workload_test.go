package rootmulti

// Workload-focused integration tests for FlatKV rootmulti: read-back
// consistency, memiavl/flatkv data equivalence, full-scan LtHash verification
// over realistic write patterns, SplitWrite read routing, and a large
// single-block determinism check that exercises FlatKV's internal parallel
// LtHash path.

import (
	"math/rand"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DualWrite read consistency — read back EVM data via SC (memiavl)
// ---------------------------------------------------------------------------

func TestFlatKVDualWriteReadConsistency(t *testing.T) {
	dir := t.TempDir()
	store, storeKeys := newTestRootMulti(t, dir, dualWriteConfig())
	defer func() { require.NoError(t, store.Close()) }()

	addrs := newMultiEVMTestData(3)

	type writtenKV struct {
		key   []byte
		value []byte
	}
	blockWrites := make(map[int64][]writtenKV)

	for block := 1; block <= 5; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)
		var writes []writtenKV

		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
		cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b, b})

		for i, addr := range addrs {
			storVal := makeSlot(b, byte(i), 0xAA)
			cms.GetKVStore(storeKeys["evm"]).Set(addr.storKey, storVal)
			writes = append(writes, writtenKV{key: addr.storKey, value: storVal})

			nonceVal := makeNonce(uint64(block*10 + i))
			cms.GetKVStore(storeKeys["evm"]).Set(addr.nonKey, nonceVal)
			writes = append(writes, writtenKV{key: addr.nonKey, value: nonceVal})

			if block == 1 {
				codeVal := []byte{0x60, 0x60, byte(i)}
				cms.GetKVStore(storeKeys["evm"]).Set(addr.codeKey, codeVal)
				writes = append(writes, writtenKV{key: addr.codeKey, value: codeVal})
			}
		}

		cms.Write()
		_, err := store.GetWorkingHash()
		require.NoError(t, err)
		cid := store.Commit(true)
		blockWrites[cid.Version] = writes
	}

	// Read back at latest version via SC memiavl child store
	evmChild := store.scStore.GetChildStoreByName("evm")
	require.NotNil(t, evmChild)
	for _, w := range blockWrites[5] {
		got := evmChild.Get(w.key)
		require.Equalf(t, w.value, got, "memiavl latest read mismatch for key %x", w.key)
		require.True(t, evmChild.Has(w.key))
	}

	// Read back at historical versions
	for v := int64(1); v <= 5; v++ {
		scStore, err := store.scStore.LoadVersion(v, true)
		require.NoError(t, err)
		child := scStore.GetChildStoreByName("evm")
		require.NotNil(t, child)

		for _, w := range blockWrites[v] {
			got := child.Get(w.key)
			require.Equalf(t, w.value, got, "memiavl v%d read mismatch for key %x", v, w.key)
		}
		require.NoError(t, scStore.Close())
	}

	// Has() for a non-existent key should return false
	fakeKey := evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, make([]byte, 52))
	require.False(t, evmChild.Has(fakeKey))
}

// ---------------------------------------------------------------------------
// DualWrite data equivalence — memiavl vs flatkv byte-for-byte
// ---------------------------------------------------------------------------

func TestFlatKVDualWriteDataEquivalence(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	addrs := newMultiEVMTestData(4)

	type writtenKV struct {
		key   []byte
		value []byte
	}
	var allWrites []writtenKV

	for block := 1; block <= 10; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)

		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
		cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b, b})

		for i, addr := range addrs {
			storVal := makeSlot(b, byte(i), 0xCC)
			cms.GetKVStore(storeKeys["evm"]).Set(addr.storKey, storVal)
			if block == 10 {
				allWrites = append(allWrites, writtenKV{key: addr.storKey, value: storVal})
			}

			nonceVal := makeNonce(uint64(block*100 + i))
			cms.GetKVStore(storeKeys["evm"]).Set(addr.nonKey, nonceVal)
			if block == 10 {
				allWrites = append(allWrites, writtenKV{key: addr.nonKey, value: nonceVal})
			}

			if block == 1 {
				codeVal := []byte{0x60, 0x60, byte(i), 0xDD}
				cms.GetKVStore(storeKeys["evm"]).Set(addr.codeKey, codeVal)
			}
		}

		cms.Write()
		_, err := store.GetWorkingHash()
		require.NoError(t, err)
		store.Commit(true)
	}

	// Read memiavl values while the store is still open (before closing
	// releases the lock).
	evmTree := store.scStore.GetChildStoreByName("evm")
	require.NotNil(t, evmTree)

	// Also collect code keys from memiavl (written in block 1, still present).
	for _, addr := range addrs {
		codeVal := evmTree.Get(addr.codeKey)
		if codeVal != nil {
			allWrites = append(allWrites, writtenKV{key: addr.codeKey, value: codeVal})
		}
	}

	memiavlValues := make(map[string][]byte)
	for _, w := range allWrites {
		got := evmTree.Get(w.key)
		memiavlValues[string(w.key)] = got
		require.Equalf(t, w.value, got, "memiavl value mismatch for key %x", w.key)
	}
	require.NoError(t, store.Close())

	ro := openFlatKVReadOnly(t, dir, cfg, 0)
	defer func() { require.NoError(t, ro.Close()) }()

	for _, w := range allWrites {
		flatkvVal, found := ro.Get(evm.EVMStoreKey, w.key)
		require.Truef(t, found, "flatkv missing key %x", w.key)
		require.Equalf(t, memiavlValues[string(w.key)], flatkvVal,
			"memiavl vs flatkv divergence for key %x:\n  memiavl: %x\n  flatkv:  %x",
			w.key, memiavlValues[string(w.key)], flatkvVal)
	}
}

// ---------------------------------------------------------------------------
// Full-scan LtHash verification at integration level
// ---------------------------------------------------------------------------

func TestFlatKVFullScanLtHashVerification(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	addrs := newMultiEVMTestData(3)

	for block := 1; block <= 10; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)

		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
		cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b})

		for i, addr := range addrs {
			cms.GetKVStore(storeKeys["evm"]).Set(addr.storKey, makeSlot(b, byte(i)))
			cms.GetKVStore(storeKeys["evm"]).Set(addr.nonKey, makeNonce(uint64(block)))
			if block == 1 {
				cms.GetKVStore(storeKeys["evm"]).Set(addr.codeKey, []byte{0x60, byte(i)})
			}
			if block%2 == 0 {
				cms.GetKVStore(storeKeys["evm"]).Set(addr.storKey, makeSlot(b, byte(i), 0xFF))
			}
		}

		cms.Write()
		_, err := store.GetWorkingHash()
		require.NoError(t, err)
		store.Commit(true)
	}

	lattice := findStoreInfo(store.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotNil(t, lattice)
	expectedLatticeHash := lattice.CommitId.Hash
	require.NoError(t, store.Close())

	ro := openFlatKVReadOnly(t, dir, cfg, 0)
	defer func() { require.NoError(t, ro.Close()) }()

	require.NoError(t, flatkv.VerifyLtHash(ro), "full-scan LtHash verification failed")

	require.Equal(t, expectedLatticeHash, ro.CommittedRootHash(),
		"flatkv CommittedRootHash should match evm_lattice in CommitInfo")
}

// ---------------------------------------------------------------------------
// Delete and overwrite workload with LtHash verification
// ---------------------------------------------------------------------------

func TestFlatKVDeleteAndOverwriteWorkload(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	addr1 := newEVMTestData(0x01)
	addr2 := newEVMTestData(0x02)
	addr3 := newEVMTestData(0x03)
	addr4 := newEVMTestData(0x04)

	records := make([]commitRecord, 0, 5)

	// Block 1: Set storage/nonce/code for 3 addresses
	{
		cms := store.CacheMultiStore()
		evmKV := cms.GetKVStore(storeKeys["evm"])
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{1})
		for _, addr := range []evmTestData{addr1, addr2, addr3} {
			evmKV.Set(addr.storKey, makeSlot(0x01, 0xAA))
			evmKV.Set(addr.nonKey, makeNonce(1))
			evmKV.Set(addr.codeKey, []byte{0x60, 0x60})
		}
		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	// Block 2: Overwrite storage values for all 3 addresses
	{
		cms := store.CacheMultiStore()
		evmKV := cms.GetKVStore(storeKeys["evm"])
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{2})
		for _, addr := range []evmTestData{addr1, addr2, addr3} {
			evmKV.Set(addr.storKey, makeSlot(0x02, 0xBB))
			evmKV.Set(addr.nonKey, makeNonce(2))
		}
		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	// Block 3: Delete storage for addr1, delete code for addr2
	{
		cms := store.CacheMultiStore()
		evmKV := cms.GetKVStore(storeKeys["evm"])
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{3})
		evmKV.Delete(addr1.storKey)
		evmKV.Delete(addr2.codeKey)
		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	// Block 4: Re-create storage for addr1 (delete-then-recreate across blocks)
	{
		cms := store.CacheMultiStore()
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{4})
		cms.GetKVStore(storeKeys["evm"]).Set(addr1.storKey, makeSlot(0x04, 0xDD))
		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	// Block 5: Same-block set-then-delete for addr4
	{
		cms := store.CacheMultiStore()
		evmKV := cms.GetKVStore(storeKeys["evm"])
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{5})
		evmKV.Set(addr4.storKey, makeSlot(0x05, 0xEE))
		evmKV.Delete(addr4.storKey)
		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	for i := 1; i < 4; i++ {
		prev := findStoreInfo(records[i-1].infos, "evm_lattice")
		curr := findStoreInfo(records[i].infos, "evm_lattice")
		require.NotNil(t, prev)
		require.NotNil(t, curr)
		require.NotEqualf(t, prev.CommitId.Hash, curr.CommitId.Hash,
			"lattice hash must change between blocks %d and %d", i, i+1)
	}
	// Block 5: set-then-delete of a non-existent key is a no-op for LtHash;
	// the hash should stay the same as block 4. The invariant relied on here
	// is pinned by a comment near ComputeLtHash in
	// sei-db/state_db/sc/flatkv/lthash/api.go.
	lattice4 := findStoreInfo(records[3].infos, "evm_lattice")
	lattice5 := findStoreInfo(records[4].infos, "evm_lattice")
	require.NotNil(t, lattice4)
	require.NotNil(t, lattice5)
	require.Equalf(t, lattice4.CommitId.Hash, lattice5.CommitId.Hash,
		"set-then-delete of non-existent key should be a no-op for LtHash")

	verifyHistoricalHashes(t, store, records)

	require.NoError(t, store.Close())
	verifyFlatKVSelfConsistent(t, dir, cfg)
}

// ---------------------------------------------------------------------------
// Multi-account workload with realistic key distribution
// ---------------------------------------------------------------------------

func TestFlatKVMultiAccountWorkload(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	const numAddrs = 20
	const slotsPerAddr = 5
	const numBlocks = 10

	type addrSlots struct {
		data  evmTestData
		extra []evmTestData
	}

	rng := rand.New(rand.NewSource(42))
	allAddrs := make([]addrSlots, numAddrs)
	for i := range allAddrs {
		seed := byte(i + 0x10)
		allAddrs[i].data = newEVMTestData(seed)
		for s := 1; s < slotsPerAddr; s++ {
			var addr [20]byte
			addr[0] = seed
			addr[19] = 0xFF
			var slot [32]byte
			slot[0] = seed + byte(s) + 1
			slot[31] = byte(s)
			internal := make([]byte, 52)
			copy(internal[:20], addr[:])
			copy(internal[20:], slot[:])
			allAddrs[i].extra = append(allAddrs[i].extra, evmTestData{
				storKey: evm.BuildMemIAVLEVMKey(evm.EVMKeyStorage, internal),
			})
		}
	}

	records := make([]commitRecord, 0, numBlocks)

	for block := 1; block <= numBlocks; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)
		evmKV := cms.GetKVStore(storeKeys["evm"])
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
		cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b})

		numToTouch := 5 + rng.Intn(numAddrs-5)
		perm := rng.Perm(numAddrs)[:numToTouch]

		for _, idx := range perm {
			as := allAddrs[idx]
			action := rng.Intn(10)

			switch {
			case action < 6: // 60%: set/overwrite
				evmKV.Set(as.data.storKey, makeSlot(b, byte(idx)))
				evmKV.Set(as.data.nonKey, makeNonce(uint64(block*100+idx)))
				if block == 1 {
					evmKV.Set(as.data.codeKey, []byte{0x60, byte(idx)})
				}
				for _, extra := range as.extra {
					evmKV.Set(extra.storKey, makeSlot(b, byte(idx), 0xEE))
				}
			case action < 8: // 20%: delete a storage key
				evmKV.Delete(as.data.storKey)
			default: // 20%: skip (no-op for this address)
			}
		}

		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	verifyHistoricalHashes(t, store, records)
	require.NoError(t, store.Close())

	verifyFlatKVSelfConsistent(t, dir, cfg)
}

// ---------------------------------------------------------------------------
// SplitWrite read routing — EVM data absent from memiavl, present in flatkv
// ---------------------------------------------------------------------------

func TestFlatKVSplitWriteReadRouting(t *testing.T) {
	dir := t.TempDir()
	cfg := splitWriteConfig()
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	addrs := newMultiEVMTestData(3)

	type writtenKV struct {
		key   []byte
		value []byte
	}
	var finalWrites []writtenKV

	for block := 1; block <= 5; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)
		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
		cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b})

		for i, addr := range addrs {
			storVal := makeSlot(b, byte(i), 0xDD)
			cms.GetKVStore(storeKeys["evm"]).Set(addr.storKey, storVal)

			nonceVal := makeNonce(uint64(block*10 + i))
			cms.GetKVStore(storeKeys["evm"]).Set(addr.nonKey, nonceVal)

			if block == 5 {
				finalWrites = append(finalWrites,
					writtenKV{key: addr.storKey, value: storVal},
					writtenKV{key: addr.nonKey, value: nonceVal},
				)
			}
		}

		cms.Write()
		_, err := store.GetWorkingHash()
		require.NoError(t, err)
		store.Commit(true)
	}
	require.NoError(t, store.Close())

	// In SplitWrite, memiavl "evm" tree should NOT have the EVM data.
	store2, _ := newTestRootMulti(t, dir, cfg)
	evmTree := store2.scStore.GetChildStoreByName("evm")
	require.NotNil(t, evmTree)

	for _, w := range finalWrites {
		got := evmTree.Get(w.key)
		require.Nilf(t, got, "memiavl should NOT contain EVM key %x in SplitWrite mode", w.key)
		require.Falsef(t, evmTree.Has(w.key), "memiavl Has() should be false for %x in SplitWrite", w.key)
	}
	require.NoError(t, store2.Close())

	// FlatKV should have the data.
	ro := openFlatKVReadOnly(t, dir, cfg, 0)
	for _, w := range finalWrites {
		val, found := ro.Get(evm.EVMStoreKey, w.key)
		require.Truef(t, found, "flatkv should contain key %x in SplitWrite mode", w.key)
		require.Equalf(t, w.value, val, "flatkv value mismatch for key %x", w.key)
	}
	require.NoError(t, ro.Close())
}

// ---------------------------------------------------------------------------
// Large single-block changeset determinism
//
// This test exercises a block whose EVM changeset is large enough (>100
// distinct kv pairs) that FlatKV's LtHash computation dispatches across
// multiple goroutines internally (see the parallel branch at
// sei-db/state_db/sc/flatkv/lthash/api.go). The test itself is single-
// threaded; its purpose is to assert that the resulting app hash is
// deterministic across independent runs, i.e. that the internal parallel
// fan-out preserves associativity and commutativity of MixIn/MixOut.
// ---------------------------------------------------------------------------

func TestFlatKVLargeChangesetDeterminism(t *testing.T) {
	const nKeys = 200
	cfg := dualWriteConfig()
	addrBase := newEVMTestData(0xAB)
	storageKeys := storageMemIAVLKeys(0xAB, nKeys)

	var hashes [2][]byte
	for run := 0; run < 2; run++ {
		dir := t.TempDir()
		store, keys := newTestRootMulti(t, dir, cfg)

		rec1 := simulateBlockManyStorage(t, store, keys, 1, storageKeys, addrBase)
		require.Equal(t, int64(1), rec1.version)
		rec2 := simulateBlockManyStorage(t, store, keys, 2, storageKeys, addrBase)
		require.Equal(t, int64(2), rec2.version)

		hashes[run] = store.lastCommitInfo.Hash()
		lattice := findStoreInfo(store.lastCommitInfo.StoreInfos, "evm_lattice")
		require.NotNil(t, lattice)

		require.NoError(t, store.Close())
		verifyFlatKVSelfConsistent(t, dir, cfg)
	}

	require.Equal(t, hashes[0], hashes[1], "large-changeset app hash must be deterministic across runs")
}
