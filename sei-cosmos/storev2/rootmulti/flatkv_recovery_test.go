package rootmulti

// Crash / rollback / reopen integration tests for the FlatKV rootmulti
// wiring. Covers rollback with lattice hash, crash recovery in both
// directions (FlatKV behind cosmos and cosmos behind FlatKV), multi-cycle
// close/reopen, rollback boundaries, sequential rollbacks, rollback followed
// by a simulated crash, and two SplitWrite-specific crash variants
// (TestFlatKVSplitWriteCrashRecovery, TestFlatKVSplitWriteReverseCrashRecovery)
// that mirror the DualWrite crash/reverse-crash cases.

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Rollback preserves lattice hash correctness
// ---------------------------------------------------------------------------

func TestFlatKVRollbackWithLatticeHash(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), dualWriteConfig())
	evmData := newEVMTestData(0x11)

	var records []commitRecord
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
	}

	require.NoError(t, store.RollbackToVersion(3))
	require.Equal(t, int64(3), store.LastCommitID().Version)
	require.Equal(t, records[2].hash, store.lastCommitInfo.Hash(),
		"after rollback to v3, app hash must match original v3")

	lattice := findStoreInfo(store.lastCommitInfo.StoreInfos, "evm_lattice")
	origLattice := findStoreInfo(records[2].infos, "evm_lattice")
	require.Equal(t, origLattice.CommitId.Hash, lattice.CommitId.Hash,
		"lattice hash must match original v3 after rollback")

	for block := 4; block <= 7; block++ {
		rec := simulateBlock(t, store, storeKeys, block+100, evmData)
		require.Equal(t, int64(block), rec.version)
		require.NotNil(t, findStoreInfo(rec.infos, "evm_lattice"))
	}

	require.NoError(t, store.Close())
}

// ---------------------------------------------------------------------------
// Crash recovery — FlatKV behind cosmos, version reconciliation
// ---------------------------------------------------------------------------

func TestFlatKVCrashRecoveryThroughRootMulti(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x22)

	store1, storeKeys1 := newTestRootMulti(t, dir, cfg)
	var records []commitRecord
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store1, storeKeys1, block, evmData))
	}
	require.NoError(t, store1.Close())

	rollbackFlatKV(t, dir, cfg, 3)

	store2, storeKeys2 := newTestRootMulti(t, dir, cfg)

	require.Equal(t, int64(3), store2.LastCommitID().Version,
		"after crash recovery, version should reconcile to 3")
	require.Equal(t, records[2].hash, store2.lastCommitInfo.Hash(),
		"after crash recovery, app hash must match original v3")

	lattice := findStoreInfo(store2.lastCommitInfo.StoreInfos, "evm_lattice")
	origLattice := findStoreInfo(records[2].infos, "evm_lattice")
	require.NotNil(t, lattice)
	require.NotNil(t, origLattice)
	require.Equal(t, origLattice.CommitId.Hash, lattice.CommitId.Hash,
		"lattice hash must match original v3 after crash recovery")

	for block := 4; block <= 8; block++ {
		rec := simulateBlock(t, store2, storeKeys2, block+200, evmData)
		require.Equal(t, int64(block), rec.version)
		require.NotNil(t, findStoreInfo(rec.infos, "evm_lattice"))
	}

	require.NoError(t, store2.Close())
}

// ---------------------------------------------------------------------------
// Crash recovery in SplitWrite — FlatKV behind cosmos
//
// TestFlatKVCrashRecoveryThroughRootMulti covers DualWrite where both
// backends hold the EVM changeset. In SplitWrite the "evm" memiavl tree is
// intentionally empty, so after the reconciliation rolls both backends to v3
// the only surviving copy of the historical EVM state lives in FlatKV. This
// test asserts that:
//
//  1. Reconciliation still converges to min(cosmos, flatkv) = 3 and that the
//     CommitInfo rebuilt from the memiavl checkpoint plus the rewound flatkv
//     row reproduces the original v3 app hash.
//  2. Historical EVM reads at v1..v3 still succeed via the FlatKV readonly
//     snapshot, proving that rollback did not silently drop EVM leaves that
//     are only stored in FlatKV.
// ---------------------------------------------------------------------------

func TestFlatKVSplitWriteCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := splitWriteConfig()
	evmData := newEVMTestData(0x33)

	store1, keys1 := newTestRootMulti(t, dir, cfg)
	var records []commitRecord
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store1, keys1, block, evmData))
	}
	require.NoError(t, store1.Close())

	// Simulate crash: FlatKV behind cosmos (memiavl at v5, flatkv at v3).
	rollbackFlatKV(t, dir, cfg, 3)

	store2, _ := newTestRootMulti(t, dir, cfg)

	require.Equal(t, int64(3), store2.LastCommitID().Version,
		"after SplitWrite crash recovery, version should reconcile to 3")
	require.Equal(t, records[2].hash, store2.lastCommitInfo.Hash(),
		"after SplitWrite crash recovery, app hash must match original v3")

	lattice := findStoreInfo(store2.lastCommitInfo.StoreInfos, "evm_lattice")
	origLattice := findStoreInfo(records[2].infos, "evm_lattice")
	require.NotNil(t, lattice)
	require.NotNil(t, origLattice)
	require.Equal(t, origLattice.CommitId.Hash, lattice.CommitId.Hash,
		"lattice hash must match original v3 after SplitWrite crash recovery")

	// SplitWrite-specific: EVM data lives only in FlatKV. After recovery the
	// v3 value must still be reachable; memiavl never held it.
	require.NoError(t, store2.Close())
	ro := openFlatKVReadOnly(t, dir, cfg, 0)
	expected := makeSlot(0x03, 0xAA) // block=3 => first byte 0x03
	got, found := ro.Get(evm.EVMStoreKey, evmData.storKey)
	require.True(t, found, "flatkv should still hold EVM key after crash + reconcile")
	require.Equal(t, expected, got,
		"flatkv value at reconciled v3 should equal block-3 value, not block-5 pre-crash value")
	require.NoError(t, ro.Close())

	// Chain must continue making progress on the reconciled stores. In
	// SplitWrite the "evm" memiavl subtree receives no writes, so its hash
	// must stay stable across the newly-committed blocks (equal to the
	// reconciled v3 hash).
	store3, keys3 := newTestRootMulti(t, dir, cfg)
	stableEVMHash := findStoreInfo(records[2].infos, "evm").CommitId.Hash
	for block := 4; block <= 7; block++ {
		rec := simulateBlock(t, store3, keys3, block+300, evmData)
		require.Equal(t, int64(block), rec.version)
		require.NotNil(t, findStoreInfo(rec.infos, "evm_lattice"))

		evmInfo := findStoreInfo(rec.infos, "evm")
		require.NotNil(t, evmInfo)
		require.Equalf(t, stableEVMHash, evmInfo.CommitId.Hash,
			"evm memiavl hash must stay stable in SplitWrite (block %d)", block)
	}
	require.NoError(t, store3.Close())
}

// ---------------------------------------------------------------------------
// Reverse crash recovery — FlatKV ahead of cosmos (reconcile rolls back EVM)
// ---------------------------------------------------------------------------

func TestFlatKVReverseCrashRecoveryFlatKVAhead(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x77)

	store1, storeKeys1 := newTestRootMulti(t, dir, cfg)
	var records []commitRecord
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store1, storeKeys1, block, evmData))
	}
	require.NoError(t, store1.Close())

	rollbackMemiavl(t, dir, cfg, 3)

	store2, storeKeys2 := newTestRootMulti(t, dir, cfg)

	require.Equal(t, int64(3), store2.LastCommitID().Version,
		"after reverse crash recovery, version should reconcile to 3")
	require.Equal(t, records[2].hash, store2.lastCommitInfo.Hash(),
		"after reverse crash recovery, app hash must match original v3")

	lattice := findStoreInfo(store2.lastCommitInfo.StoreInfos, "evm_lattice")
	origLattice := findStoreInfo(records[2].infos, "evm_lattice")
	require.NotNil(t, lattice)
	require.NotNil(t, origLattice)
	require.Equal(t, origLattice.CommitId.Hash, lattice.CommitId.Hash,
		"lattice hash must match original v3 after reverse crash recovery")

	for block := 4; block <= 8; block++ {
		rec := simulateBlock(t, store2, storeKeys2, block+200, evmData)
		require.Equal(t, int64(block), rec.version)
		require.NotNil(t, findStoreInfo(rec.infos, "evm_lattice"))
	}

	require.NoError(t, store2.Close())
}

// ---------------------------------------------------------------------------
// Reverse crash recovery in SplitWrite — FlatKV ahead of cosmos
//
// Symmetric to TestFlatKVReverseCrashRecoveryFlatKVAhead but for SplitWrite.
// Because memiavl "evm" never held the EVM changeset in the first place,
// reconciling cosmos up to v5 and rewinding FlatKV back to v3 has to land
// the chain at v3 with the v3 EVM value still readable from FlatKV.
// ---------------------------------------------------------------------------

func TestFlatKVSplitWriteReverseCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := splitWriteConfig()
	evmData := newEVMTestData(0x78)

	store1, keys1 := newTestRootMulti(t, dir, cfg)
	var records []commitRecord
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store1, keys1, block, evmData))
	}
	require.NoError(t, store1.Close())

	// Simulate crash: cosmos rolled back to v3 while FlatKV still at v5.
	rollbackMemiavl(t, dir, cfg, 3)

	store2, _ := newTestRootMulti(t, dir, cfg)
	require.Equal(t, int64(3), store2.LastCommitID().Version,
		"after SplitWrite reverse crash recovery, version should reconcile to 3")
	require.Equal(t, records[2].hash, store2.lastCommitInfo.Hash(),
		"after SplitWrite reverse crash recovery, app hash must match original v3")

	lattice := findStoreInfo(store2.lastCommitInfo.StoreInfos, "evm_lattice")
	origLattice := findStoreInfo(records[2].infos, "evm_lattice")
	require.NotNil(t, lattice)
	require.NotNil(t, origLattice)
	require.Equal(t, origLattice.CommitId.Hash, lattice.CommitId.Hash,
		"lattice hash must match original v3 after SplitWrite reverse crash recovery")

	require.NoError(t, store2.Close())

	// FlatKV must hold the v3 value, not the pre-crash v5 value.
	ro := openFlatKVReadOnly(t, dir, cfg, 0)
	expected := makeSlot(0x03, 0xAA)
	got, found := ro.Get(evm.EVMStoreKey, evmData.storKey)
	require.True(t, found, "flatkv should hold EVM key at reconciled v3")
	require.Equal(t, expected, got,
		"flatkv value must be rewound to v3 (not v5) after reverse reconciliation")
	require.NoError(t, ro.Close())

	store3, keys3 := newTestRootMulti(t, dir, cfg)
	stableEVMHash := findStoreInfo(records[2].infos, "evm").CommitId.Hash
	for block := 4; block <= 7; block++ {
		rec := simulateBlock(t, store3, keys3, block+400, evmData)
		require.Equal(t, int64(block), rec.version)

		evmInfo := findStoreInfo(rec.infos, "evm")
		require.NotNil(t, evmInfo)
		require.Equalf(t, stableEVMHash, evmInfo.CommitId.Hash,
			"evm memiavl hash must stay stable in SplitWrite (block %d)", block)
	}
	require.NoError(t, store3.Close())
}

// ---------------------------------------------------------------------------
// Multiple close/reopen cycles with interleaved writes
// ---------------------------------------------------------------------------

func TestFlatKVMultiCloseReopenCycles(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x66)

	var records []commitRecord

	// Cycle 1: blocks 1–2
	{
		store, keys := newTestRootMulti(t, dir, cfg)
		for block := 1; block <= 2; block++ {
			records = append(records, simulateBlock(t, store, keys, block, evmData))
		}
		require.NoError(t, store.Close())
	}

	// Cycle 2: reopen, blocks 3–4
	{
		store, keys := newTestRootMulti(t, dir, cfg)
		require.Equal(t, int64(2), store.LastCommitID().Version)
		require.Equal(t, records[1].hash, store.lastCommitInfo.Hash())
		for block := 3; block <= 4; block++ {
			records = append(records, simulateBlock(t, store, keys, block, evmData))
		}
		require.NoError(t, store.Close())
	}

	// Cycle 3: reopen, blocks 5–6
	{
		store, keys := newTestRootMulti(t, dir, cfg)
		require.Equal(t, int64(4), store.LastCommitID().Version)
		require.Equal(t, records[3].hash, store.lastCommitInfo.Hash())
		for block := 5; block <= 6; block++ {
			records = append(records, simulateBlock(t, store, keys, block, evmData))
		}
		verifyHistoricalHashes(t, store, records)
		require.NoError(t, store.Close())
	}

	require.Len(t, records, 6)
	verifyFlatKVSelfConsistent(t, dir, cfg)
}

// ---------------------------------------------------------------------------
// Rollback to version 1 (earliest boundary)
// ---------------------------------------------------------------------------

func TestFlatKVRollbackToVersionOne(t *testing.T) {
	store, storeKeys := newTestRootMulti(t, t.TempDir(), dualWriteConfig())
	evmData := newEVMTestData(0x55)

	var records []commitRecord
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
	}

	require.NoError(t, store.RollbackToVersion(1))
	require.Equal(t, int64(1), store.LastCommitID().Version)
	require.Equal(t, records[0].hash, store.lastCommitInfo.Hash())

	lattice := findStoreInfo(store.lastCommitInfo.StoreInfos, "evm_lattice")
	require.Equal(t, findStoreInfo(records[0].infos, "evm_lattice").CommitId.Hash, lattice.CommitId.Hash)

	// Re-commit blocks 2–5: should reproduce original hashes
	for block := 2; block <= 5; block++ {
		rec := simulateBlock(t, store, storeKeys, block, evmData)
		require.Equal(t, records[block-1].hash, rec.hash,
			"replayed block %d app hash should match original", block)
		require.Equal(t,
			findStoreInfo(records[block-1].infos, "evm_lattice").CommitId.Hash,
			findStoreInfo(rec.infos, "evm_lattice").CommitId.Hash,
			"replayed block %d lattice hash should match original", block)
	}

	require.NoError(t, store.Close())
}

// ---------------------------------------------------------------------------
// Multiple sequential rollbacks
// ---------------------------------------------------------------------------

func TestFlatKVSequentialRollbacks(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x42)
	store, storeKeys := newTestRootMulti(t, dir, cfg)

	var records []commitRecord
	for block := 1; block <= 10; block++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
	}

	require.NoError(t, store.RollbackToVersion(7))
	require.Equal(t, int64(7), store.LastCommitID().Version)
	require.Equal(t, records[6].hash, store.lastCommitInfo.Hash())

	for block := 8; block <= 9; block++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, evmData))
	}

	require.NoError(t, store.RollbackToVersion(5))
	require.Equal(t, int64(5), store.LastCommitID().Version)
	require.Equal(t, records[4].hash, store.lastCommitInfo.Hash())

	rec6 := simulateBlock(t, store, storeKeys, 10, evmData)
	require.Equal(t, int64(6), rec6.version)

	require.NoError(t, store.Close())

	verifyFlatKVSelfConsistent(t, dir, cfg)
}

// ---------------------------------------------------------------------------
// Rollback then simulated crash (cosmos ahead of FlatKV)
// ---------------------------------------------------------------------------

func TestFlatKVRollbackThenCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := dualWriteConfig()
	evmData := newEVMTestData(0x99)

	var records []commitRecord
	store1, keys1 := newTestRootMulti(t, dir, cfg)
	for block := 1; block <= 5; block++ {
		records = append(records, simulateBlock(t, store1, keys1, block, evmData))
	}

	require.NoError(t, store1.RollbackToVersion(3))
	require.Equal(t, int64(3), store1.LastCommitID().Version)

	records = records[:3]
	records = append(records, simulateBlock(t, store1, keys1, 4, evmData))
	require.NoError(t, store1.Close())

	// Cosmos at v4, FlatKV still at v3 (crash after cosmos commit)
	rollbackFlatKV(t, dir, cfg, 3)

	store2, keys2 := newTestRootMulti(t, dir, cfg)
	require.Equal(t, int64(3), store2.LastCommitID().Version)
	require.Equal(t, records[2].hash, store2.lastCommitInfo.Hash())

	lattice := findStoreInfo(store2.lastCommitInfo.StoreInfos, "evm_lattice")
	require.Equal(t, findStoreInfo(records[2].infos, "evm_lattice").CommitId.Hash, lattice.CommitId.Hash)

	records = records[:3]
	records = append(records, simulateBlock(t, store2, keys2, 4+200, evmData))

	for block := 5; block <= 7; block++ {
		records = append(records, simulateBlock(t, store2, keys2, block+200, evmData))
	}

	require.NoError(t, store2.Close())
}
