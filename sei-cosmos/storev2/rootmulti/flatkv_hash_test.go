package rootmulti

// Lattice hash behavior: determinism, sensitivity, double-flush, mode parity.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestOnlyDualWrite + LatticeHash — hash consistency through rootmulti
// ---------------------------------------------------------------------------

func TestFlatKVDualWriteHashConsistency(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), dualWriteConfig())
	defer func() { require.NoError(t, store.Close()) }()

	evmData := newEVMTestData(0xAA)
	var records []commitRecord

	for block := 1; block <= 10; block++ {
		rec := simulateBlock(t, store, storeKeys, block, evmData)
		records = append(records, rec)

		lattice := findStoreInfo(rec.infos, "evm_lattice")
		require.NotNilf(t, lattice, "evm_lattice missing at block %d", block)
		require.Lenf(t, lattice.CommitId.Hash, 32, "lattice hash should be 32 bytes at block %d", block)
	}

	for i := 1; i < len(records); i++ {
		prev := findStoreInfo(records[i-1].infos, "evm_lattice")
		curr := findStoreInfo(records[i].infos, "evm_lattice")
		require.NotEqual(t, prev.CommitId.Hash, curr.CommitId.Hash,
			"lattice hash must change between blocks %d and %d", i, i+1)
	}

	verifyHistoricalHashes(t, store, records)
}

// ---------------------------------------------------------------------------
// EVMMigrated — hash consistency, EVM data not in memiavl tree
// ---------------------------------------------------------------------------

func TestFlatKVEVMMigratedHashConsistency(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), evmMigratedConfig())
	defer func() { require.NoError(t, store.Close()) }()

	evmData := newEVMTestData(0xBB)
	var records []commitRecord

	for block := 1; block <= 10; block++ {
		rec := simulateBlock(t, store, storeKeys, block, evmData)
		records = append(records, rec)

		lattice := findStoreInfo(rec.infos, "evm_lattice")
		require.NotNilf(t, lattice, "evm_lattice missing at block %d", block)
		require.NotEmpty(t, lattice.CommitId.Hash)

		// In EVMMigrated the "evm" memiavl tree receives no data; its IAVL hash
		// must remain unchanged across blocks.
		if block > 1 {
			prev := findStoreInfo(records[block-2].infos, "evm")
			curr := findStoreInfo(rec.infos, "evm")
			require.Equal(t, prev.CommitId.Hash, curr.CommitId.Hash,
				"evm IAVL hash should not change in EVMMigrated mode (block %d)", block)
		}
	}

	verifyHistoricalHashes(t, store, records)
}

// ---------------------------------------------------------------------------
// Determinism — two stores with identical data produce identical hashes
// ---------------------------------------------------------------------------

func TestFlatKVLatticeHashDeterminism(t *testing.T) {
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0xCC)

	var hashes [2][]byte
	var latticeHashes [2][]byte

	for i := 0; i < 2; i++ {
		store, storeKeys := newTestRootMulti(t, t.TempDir(), cfg)
		for block := 1; block <= 5; block++ {
			simulateBlock(t, store, storeKeys, block, evmData)
		}
		hashes[i] = store.lastCommitInfo.Hash()
		lattice := findStoreInfo(store.lastCommitInfo.StoreInfos, "evm_lattice")
		require.NotNil(t, lattice)
		latticeHashes[i] = lattice.CommitId.Hash
		require.NoError(t, store.Close())
	}

	require.Equal(t, hashes[0], hashes[1], "app hashes must be deterministic")
	require.Equal(t, latticeHashes[0], latticeHashes[1], "lattice hashes must be deterministic")
}

// ---------------------------------------------------------------------------
// Sensitivity — single byte change in EVM data changes lattice hash
// ---------------------------------------------------------------------------

func TestFlatKVLatticeHashSensitivity(t *testing.T) {
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0xDD)

	storeA, keysA := newTestRootMulti(t, t.TempDir(), cfg)
	for block := 1; block <= 3; block++ {
		simulateBlock(t, storeA, keysA, block, evmData)
	}

	storeB, keysB := newTestRootMulti(t, t.TempDir(), cfg)
	for block := 1; block <= 3; block++ {
		if block == 3 {
			cms := storeB.CacheMultiStore()
			cms.GetKVStore(keysB["acc"]).Set([]byte("acct1"), []byte{byte(block)})
			cms.GetKVStore(keysB["bank"]).Set([]byte("supply"), []byte{byte(block), byte(block)})
			// 0xBB instead of 0xAA — single byte difference
			cms.GetKVStore(keysB["evm"]).Set(evmData.storKey, makeSlot(byte(block), 0xBB))
			cms.GetKVStore(keysB["evm"]).Set(evmData.nonKey, makeNonce(uint64(block)))
			cms.Write()
			_, err := storeB.GetWorkingHash()
			require.NoError(t, err)
			storeB.Commit(true)
		} else {
			simulateBlock(t, storeB, keysB, block, evmData)
		}
	}

	latticeA := findStoreInfo(storeA.lastCommitInfo.StoreInfos, "evm_lattice")
	latticeB := findStoreInfo(storeB.lastCommitInfo.StoreInfos, "evm_lattice")
	require.NotEqual(t, latticeA.CommitId.Hash, latticeB.CommitId.Hash,
		"lattice hash must differ when EVM data differs by a single byte")
	require.NotEqual(t, storeA.lastCommitInfo.Hash(), storeB.lastCommitInfo.Hash(),
		"app hash must differ when lattice hash differs")

	require.NoError(t, storeA.Close())
	require.NoError(t, storeB.Close())
}

// ---------------------------------------------------------------------------
// Double flush (FinalizeBlock + Commit) with TestOnlyDualWrite
// ---------------------------------------------------------------------------

func TestFlatKVDualWriteDoubleFlush(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), dualWriteConfig())
	defer func() { require.NoError(t, store.Close()) }()

	evmData := newEVMTestData(0xEE)
	var records []commitRecord

	for block := 1; block <= 5; block++ {
		cms := store.CacheMultiStore()
		b := byte(block)
		evmKV := cms.GetKVStore(storeKeys["evm"])

		cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
		cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b, b})
		evmKV.Set(evmData.storKey, makeSlot(b, 0xAA))
		evmKV.Set(evmData.nonKey, makeNonce(uint64(block)))

		// Simulate FinalizeBlock: Write + GetWorkingHash
		cms.Write()
		_, err := store.GetWorkingHash()
		require.NoError(t, err)

		// Simulate Commit: second Write + GetWorkingHash + Commit (double flush)
		cms.Write()
		records = append(records, finalizeBlock(t, store))
	}

	for _, rec := range records {
		scStore, err := store.scStore.LoadVersion(rec.version, true)
		require.NoError(t, err)

		commitInfo := convertCommitInfo(scStore.LastCommitInfo())
		commitInfo = amendCommitInfo(commitInfo, store.storesParams)
		require.Equalf(t, rec.hash, commitInfo.Hash(),
			"ROOT HASH MISMATCH at version %d (double flush)", rec.version)

		lattice := findStoreInfo(commitInfo.StoreInfos, "evm_lattice")
		require.NotNilf(t, lattice, "evm_lattice must survive double flush at version %d", rec.version)
		_ = scStore.Close()
	}
}

// ---------------------------------------------------------------------------
// Mixed cosmos+EVM blocks — selective lattice hash changes
//
// Also subsumes a standalone "empty EVM blocks" case: the per-block assertions
// below pin both directions of the invariant — lattice hash stays byte-equal
// across cosmos-only blocks and must change across EVM-write blocks — so a
// separate fixed-sequence test adds no additional signal.
// ---------------------------------------------------------------------------

func TestFlatKVMixedCosmosAndEVMBlocks(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), dualWriteConfig())
	defer func() { require.NoError(t, store.Close()) }()

	evmData := newEVMTestData(0x55)
	var records []commitRecord

	for block := 1; block <= 10; block++ {
		if block%2 == 1 {
			records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
		} else {
			records = append(records, simulateCosmosOnlyBlock(t, store, storeKeys, block))
		}
	}

	verifyHistoricalHashes(t, store, records)

	for i := 1; i < len(records); i++ {
		prev := findStoreInfo(records[i-1].infos, "evm_lattice")
		curr := findStoreInfo(records[i].infos, "evm_lattice")
		require.NotNil(t, prev)
		require.NotNil(t, curr)

		block := i + 1
		if block%2 == 1 {
			require.NotEqualf(t, prev.CommitId.Hash, curr.CommitId.Hash,
				"lattice hash should change on EVM-write block %d", block)
		} else {
			require.Equalf(t, prev.CommitId.Hash, curr.CommitId.Hash,
				"lattice hash should be stable on cosmos-only block %d", block)
		}
	}
}
