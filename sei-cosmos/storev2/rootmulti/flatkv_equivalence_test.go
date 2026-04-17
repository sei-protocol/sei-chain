package rootmulti

// DualWrite cross-backend differential oracle (memiavl == flatkv).

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/stretchr/testify/require"
)

// codeHashLen must match vtype.CodeHashLen. Hardcoded here to avoid pulling
// the vtype package into tests solely for one constant.
const codeHashLen = 32

// ---------------------------------------------------------------------------
// Workload key builders
// ---------------------------------------------------------------------------

func addr20(seed byte) []byte {
	a := make([]byte, 20)
	a[0] = seed
	a[19] = 0xFF
	return a
}

func storageKeyAt(addrSeed byte, slotIdx uint64) []byte {
	internal := make([]byte, 52)
	copy(internal[:20], addr20(addrSeed))
	binary.BigEndian.PutUint64(internal[44:], slotIdx)
	return keys.BuildEVMKey(keys.EVMKeyStorage, internal)
}

func nonceKeyOf(addrSeed byte) []byte {
	return keys.BuildEVMKey(keys.EVMKeyNonce, addr20(addrSeed))
}

func codeHashKeyOf(addrSeed byte) []byte {
	return keys.BuildEVMKey(keys.EVMKeyCodeHash, addr20(addrSeed))
}

func codeKeyOf(addrSeed byte) []byte {
	return keys.BuildEVMKey(keys.EVMKeyCode, addr20(addrSeed))
}

func makeCodeHash(pattern byte) []byte {
	h := make([]byte, codeHashLen)
	h[0] = pattern
	h[31] = 0xBB
	return h
}

// drives the exhaustive multi-block equivalence workload. The order of
// writes is chosen to exercise:
//
//   - first-write / overwrite paths for each EVM key kind
//   - storage-delete-then-recreate
//   - multi-slot per address (storage prefix/iterator path)
//   - empty EVM block (only non-EVM writes) as a control
//   - code deploy + code update (LegacyDB and codeDB paths)
//
// The workload deliberately pairs every codeHash write with a nonce write on
// the same address. FlatKV's accountDB row unconditionally emits a nonce
// node on export; if a codeHash is set without a corresponding nonce write,
// memiavl would have no nonce key for that address and the reverse check
// would (correctly) flag a real divergence. For this oracle workload we
// only want to catch *unintended* divergences, so we keep the invariant.
func runEquivalenceWorkload(t *testing.T, store *Store, keys map[string]*types.KVStoreKey, numAddrs byte) {
	t.Helper()

	commit := func() {
		_, err := store.GetWorkingHash()
		require.NoError(t, err)
		store.Commit(true)
	}

	// block 1 — bootstrap: nonce+codehash+code+storage slots 0..2 per addr
	cms := store.CacheMultiStore()
	evmKV := cms.GetKVStore(keys["evm"])
	cms.GetKVStore(keys["acc"]).Set([]byte("acct1"), []byte{1})
	cms.GetKVStore(keys["bank"]).Set([]byte("supply"), []byte{1, 1})
	for seed := byte(1); seed <= numAddrs; seed++ {
		evmKV.Set(nonceKeyOf(seed), makeNonce(uint64(100+seed)))
		evmKV.Set(codeHashKeyOf(seed), makeCodeHash(seed))
		evmKV.Set(codeKeyOf(seed), []byte{0x60, 0x60, seed, 0xDD})
		for slot := uint64(0); slot < 3; slot++ {
			evmKV.Set(storageKeyAt(seed, slot), makeSlot(1, seed, byte(slot), 0xAA))
		}
	}
	cms.Write()
	commit()

	// block 2 — overwrite some storage, bump nonces
	cms = store.CacheMultiStore()
	evmKV = cms.GetKVStore(keys["evm"])
	cms.GetKVStore(keys["acc"]).Set([]byte("acct1"), []byte{2})
	for seed := byte(1); seed <= numAddrs; seed++ {
		evmKV.Set(nonceKeyOf(seed), makeNonce(uint64(200+seed)))
		evmKV.Set(storageKeyAt(seed, 0), makeSlot(2, seed, 0x00, 0xAA))
	}
	cms.Write()
	commit()

	// block 3 — delete storage slot 0 for odd addrs, add slot 3 for all
	cms = store.CacheMultiStore()
	evmKV = cms.GetKVStore(keys["evm"])
	for seed := byte(1); seed <= numAddrs; seed++ {
		if seed%2 == 1 {
			evmKV.Delete(storageKeyAt(seed, 0))
		}
		evmKV.Set(storageKeyAt(seed, 3), makeSlot(3, seed, 0x03, 0xAA))
		evmKV.Set(nonceKeyOf(seed), makeNonce(300+uint64(seed)))
	}
	cms.Write()
	commit()

	// block 4 — code update + codehash update (paired with nonce bump)
	cms = store.CacheMultiStore()
	evmKV = cms.GetKVStore(keys["evm"])
	for seed := byte(1); seed <= numAddrs; seed++ {
		evmKV.Set(codeKeyOf(seed), []byte{0x60, 0x80, 0x60, 0x40, seed})
		evmKV.Set(codeHashKeyOf(seed), makeCodeHash(seed+0x10))
		evmKV.Set(nonceKeyOf(seed), makeNonce(400+uint64(seed)))
	}
	cms.Write()
	commit()

	// block 5 — re-create slot 0 for addrs previously deleted in block 3
	cms = store.CacheMultiStore()
	evmKV = cms.GetKVStore(keys["evm"])
	for seed := byte(1); seed <= numAddrs; seed++ {
		if seed%2 == 1 {
			evmKV.Set(storageKeyAt(seed, 0), makeSlot(5, seed, 0xFE, 0xED))
		}
	}
	cms.Write()
	commit()

	// block 6 — non-EVM only (control: must leave FlatKV untouched)
	cms = store.CacheMultiStore()
	cms.GetKVStore(keys["acc"]).Set([]byte("acct1"), []byte{6})
	cms.GetKVStore(keys["bank"]).Set([]byte("supply"), []byte{6, 6})
	cms.Write()
	commit()

	// block 7 — mass storage spray on a single addr to exercise bulk/prefix
	cms = store.CacheMultiStore()
	evmKV = cms.GetKVStore(keys["evm"])
	target := byte(1)
	for slot := uint64(10); slot < 40; slot++ {
		evmKV.Set(storageKeyAt(target, slot), makeSlot(7, target, byte(slot), 0xCD))
	}
	evmKV.Set(nonceKeyOf(target), makeNonce(uint64(700)))
	cms.Write()
	commit()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFlatKVDualWriteExhaustiveEquivalence drives a varied multi-block EVM
// workload through rootmulti in DualWrite mode, then iterates memiavl's evm
// child store in full and drains FlatKV's exporter in full at the latest
// version. The two snapshots must be byte-for-byte equal in both directions.
//
// This is the strongest correctness check we have for FlatKV at the
// integration layer: any divergence failing this test is a real bug, because
// memiavl received the exact same changesets FlatKV did.
func TestFlatKVDualWriteExhaustiveEquivalence(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	store, keys := newTestRootMulti(t, dir, cfg)

	runEquivalenceWorkload(t, store, keys, 5)

	// Snapshot memiavl evm state while the store is still open — closing
	// the rootmulti store releases the memiavl lock so we read now.
	evmTree := store.scStore.GetChildStoreByName("evm")
	require.NotNil(t, evmTree)
	memSet := collectCommitKVStore(t, evmTree)
	require.NotEmptyf(t, memSet, "memiavl evm store should be non-empty")

	require.NoError(t, store.Close())

	flatSet := collectFlatKVEVM(t, dir, cfg, 0)
	requireSameEVMKVSet(t, memSet, flatSet)
}

// TestFlatKVDualWriteEquivalenceAtHistoricalVersions runs a shorter workload
// and asserts equivalence at every historical version, not only the latest.
// This catches bugs where FlatKV's per-version snapshot/LoadVersion path
// drifts from memiavl's version history even if the latest state happens to
// agree.
func TestFlatKVDualWriteEquivalenceAtHistoricalVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	store, keys := newTestRootMulti(t, dir, cfg)

	runEquivalenceWorkload(t, store, keys, 3)

	latest := store.scStore.Version()
	require.Greater(t, latest, int64(1))

	// Collect a memiavl snapshot at every committed version while the
	// rootmulti store is still open (LoadVersion requires the outer store
	// to own the directory).
	memByVersion := make(map[int64]map[string][]byte, latest)
	for v := int64(1); v <= latest; v++ {
		sc, err := store.scStore.LoadVersion(v, true)
		require.NoErrorf(t, err, "memiavl LoadVersion(%d)", v)
		child := sc.GetChildStoreByName("evm")
		require.NotNilf(t, child, "memiavl evm child missing at v%d", v)
		memByVersion[v] = collectCommitKVStore(t, child)
		require.NoError(t, sc.Close())
	}

	require.NoError(t, store.Close())

	for v := int64(1); v <= latest; v++ {
		flatSet := collectFlatKVEVM(t, dir, cfg, v)
		t.Run("", func(t *testing.T) {
			requireSameEVMKVSet(t, memByVersion[v], flatSet)
		})
	}
}
