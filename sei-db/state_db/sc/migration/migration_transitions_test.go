package migration

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// Test the MigrateEVM data migration. At the start of this migration, all data lives in memIAVL.
// At the end of this migration, all evm/ data lives in flatkv, and all other data remains in memIAVL.
//
// This test evaluates the FlatKV EVM migrate (0 -> 1) path.
func TestMigrateEVM(t *testing.T) {

	rng := testutil.NewTestRandom()

	// Reserve stable directories so we can close and reopen the stores
	// mid-migration to simulate a process restart.
	memiavlDir := t.TempDir()
	flatKVDir := t.TempDir()

	// All data is initially in memiavl. Migration metadata lives
	// exclusively on flatkv; memiavl is opened with just the production
	// module stores.
	memiavlDB := NewTestMemIAVLCommitStore(t, memiavlDir, keys.MemIAVLStoreKeys)
	memiavlRouter := NewTestMemIAVLRouter(t, memiavlDB)

	// Empty flatKV store; the migration will populate it with EVM keys
	// and a single MigrationStore version-key entry.
	flatKVDB := NewTestFlatKVCommitStore(t, flatKVDir)

	// Oracle: in-memory map of (store, key) -> value. Drives the post-run
	// equivalence check.
	inMemoryRouter := NewTestInMemoryRouter()

	keysInUse := newLiveKeySet()

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// Phase 1 (v0 baseline): populate memiavl with data across all modules.
	// The multiRouter only contains memiavl + oracle, so no changesets reach
	// flatKV; commitBoth still calls flatKVDB.Commit() each block, advancing
	// its version in lockstep against an empty changeset.
	SimulateBlocks(t,
		NewTestMultiRouter(t, memiavlRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		100, // new keys per block
		100, // blocks to simulate
	)

	// Build a migration router that will migrate the evm/ data to flatkv.
	migrationRouter, err := BuildRouter(t.Context(), types.MigrateEVM, memiavlDB, flatKVDB, 100)
	require.NoError(t, err)

	// Phase 2: drive 2 blocks through the migration router. Phase 1 produced
	// ~500 EVM keys (1 of 20 modules at 100 new keys/block * 100 blocks);
	// with a batch size of 100 the migration drains those source keys in 5
	// blocks, so 2 blocks is deliberately short to leave the migration in
	// flight at the restart point: ~200 of the ~500 source keys migrated to
	// flatKV, ~300 still un-migrated in memiavl. AssertMigrationInFlight
	// below verifies this split before we close the DBs.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		2,   // blocks to simulate
	)

	// Sanity check: the test must actually catch the migration in flight,
	// otherwise the restart below is degenerate (no boundary to resume from).
	inMemoryRouter.AssertMigrationInFlight(t, memiavlDB, flatKVDB, keys.EVMStoreKey)

	// Close and reopen both backends. SimulateBlocks committed after every
	// block, so the on-disk state is consistent. The reopened router must
	// recover the migration manager's state from disk metadata - the
	// boundary key (migration cursor) and the source version stored in
	// flatKV.
	require.NoError(t, memiavlDB.Close(), "close memiavl before restart")
	require.NoError(t, flatKVDB.Close(), "close flatKV before restart")

	memiavlDB = NewTestMemIAVLCommitStore(t, memiavlDir, keys.MemIAVLStoreKeys)
	memiavlRouter = NewTestMemIAVLRouter(t, memiavlDB)
	flatKVDB = NewTestFlatKVCommitStore(t, flatKVDir)

	// Re-declare commitBoth for visual continuity. Strictly speaking the
	// original closure already observes the rebound memiavlDB / flatKVDB
	// (Go closures capture local variables by reference), but redeclaring
	// keeps the post-restart setup symmetric with phase 1.
	commitBoth = func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// Rebuild the migration router on top of the freshly-reopened DBs. The
	// manager recovers its state from disk - either resuming from the
	// boundary, or coming up in passthrough if the version key already
	// records the target version.
	migrationRouter, err = BuildRouter(t.Context(), types.MigrateEVM, memiavlDB, flatKVDB, 100)
	require.NoError(t, err, "rebuild migration router after restart")

	// Sanity check: all oracle data is still reachable through the rebuilt
	// router. Exercises the post-restart hybrid read path: ~200 EVM keys
	// already in flatKV, ~300 still in memiavl awaiting migration.
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// Phase 3: 100 more blocks after the restart. The first ~3 blocks finish
	// draining the ~300 un-migrated source EVM keys (batch 100); the
	// remaining ~97 blocks run in passthrough mode and exercise normal
	// user-key churn against the post-completion write path.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		100, // blocks to simulate
	)

	// All oracle data must be reachable through the migration router.
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// Count EVM vs non-EVM keys in the oracle.
	var evmKeyCount, nonEVMKeyCount int64
	for _, kp := range keysInUse.keys {
		if kp.store == keys.EVMStoreKey {
			evmKeyCount++
		} else {
			nonEVMKeyCount++
		}
	}

	// Key count check.
	// flatKV holds EVM data + exactly 1 migration metadata key (MigrationVersionKey).
	// MigrationBoundaryKey is deleted on the final migration block, leaving only the version key.
	// memiavl holds only non-EVM keys; its MigrationStore tree is empty (version written to flatKV).
	require.Equal(t, evmKeyCount+1, GetFlatKVKeyCount(t, flatKVDB),
		"flatKV should contain EVM keys plus one migration version metadata key")
	require.Equal(t, nonEVMKeyCount, GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl should contain only non-EVM keys")

	// Migration version check.
	flatKVVersion, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.True(t, found, "migration version key must be present in flatKV after migration")
	require.Equal(t, uint64(Version1_MigrateEVM), flatKVVersion,
		"flatKV migration version should be Version1_MigrateEVM")

	// Migration boundary check. The boundary key tracks the in-progress
	// migration cursor. On the final migration block it is deleted in the same
	// atomic write that records the new version, so post-completion it must be
	// absent from flatkv (memiavl never holds migration metadata).
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found,
		"migration boundary key must be cleared from flatKV after migration completes")

	// Placement check: each oracle key must be in the correct backend and absent from the other.
	inMemoryRouter.VerifyKeyPlacement(t, memiavlDB, flatKVDB,
		map[string]bool{keys.EVMStoreKey: true},
	)

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the MigrateAllButBank data migration. At the start of this migration,
// evm/ data lives in flatkv and everything else lives in memiavl (i.e. the
// EVMMigrated steady state). At the end, every module except bank/ lives in
// flatkv; bank/ remains in memiavl.
//
// This test evaluates the 1->2 migration path. The setup phase relies on the
// EVMMigrated router (verified by TestEVMMigrated) to lay down a realistic
// v1 schema, then explicitly seeds flatKV's MigrationVersionKey since the
// EVMMigrated router does not itself write that bookkeeping.
func TestMigrateAllButBank(t *testing.T) {

	rng := testutil.NewTestRandom()

	// Reserve stable directories so we can close and reopen the stores
	// mid-migration to simulate a process restart.
	memiavlDir := t.TempDir()
	flatKVDir := t.TempDir()

	// Migration metadata lives exclusively on flatkv; memiavl is opened
	// with just the production module stores. SimulateBlocks is
	// restricted to MemIAVLStoreKeys so no user data lands elsewhere.
	memiavlDB := NewTestMemIAVLCommitStore(t, memiavlDir, keys.MemIAVLStoreKeys)
	flatKVDB := NewTestFlatKVCommitStore(t, flatKVDir)

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// --- Phase 1: EVMMigrated setup ---
	// Lay down v1 state: evm/ in flatKV, everything else in memiavl. Drives
	// roughly equal load across all real modules so the non-evm-non-bank
	// stores accumulate enough keys to make the v1->v2 migration meaningful.
	evmMigratedRouter, err := BuildRouter(t.Context(), types.EVMMigrated, memiavlDB, flatKVDB, 0)
	require.NoError(t, err, "build EVMMigrated router")
	SimulateBlocks(t,
		NewTestMultiRouter(t, evmMigratedRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		100, // new keys per block
		100, // blocks to simulate
	)

	// The EVMMigrated router has no route for MigrationStore, so it never
	// writes the migration version key. A real chain at v1 would have that
	// key already (left behind by the prior MigrateEVM run); seed it
	// directly so the upcoming MigrateAllButBank constructor can read v1
	// from the new DB instead of erroring out.
	SeedMigrationVersionInFlatKV(t, flatKVDB, Version1_MigrateEVM)

	// --- Phase 2: partial MigrateAllButBank ---
	migrationRouter, err := BuildRouter(t.Context(), types.MigrateAllButBank, memiavlDB, flatKVDB, 100)
	require.NoError(t, err, "build MigrateAllButBank router")

	// 50 blocks * 100 batch ≈ 5,000 keys migrated, well short of the ~9,000
	// non-evm-non-bank keys produced in setup; this guarantees we end this
	// phase with a partially-migrated state and a persisted boundary.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		50,  // blocks to simulate
	)

	// Sanity check: the test must actually catch the migration in flight,
	// otherwise the restart below is degenerate (no boundary to resume
	// from). Spans every store currently being migrated - i.e. every
	// production module except bank.
	migratingStores := make([]string, 0, len(keys.MemIAVLStoreKeys)-1)
	for _, s := range keys.MemIAVLStoreKeys {
		if s != keys.BankStoreKey {
			migratingStores = append(migratingStores, s)
		}
	}
	inMemoryRouter.AssertMigrationInFlight(t, memiavlDB, flatKVDB, migratingStores...)

	// --- Restart ---
	// Close and reopen both backends to verify the in-progress migration is
	// not corrupted by a restart and resumes from the persisted boundary.
	// SimulateBlocks already committed after each block, so closing here is
	// safe.
	require.NoError(t, memiavlDB.Close(), "close memiavl before restart")
	require.NoError(t, flatKVDB.Close(), "close flatKV before restart")

	memiavlDB = NewTestMemIAVLCommitStore(t, memiavlDir, keys.MemIAVLStoreKeys)
	flatKVDB = NewTestFlatKVCommitStore(t, flatKVDir)

	commitBoth = func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	migrationRouter, err = BuildRouter(t.Context(), types.MigrateAllButBank, memiavlDB, flatKVDB, 100)
	require.NoError(t, err, "rebuild MigrateAllButBank router after restart")

	// Sanity check: all oracle data is still reachable through the rebuilt
	// router. This exercises the post-restart hybrid read path (some keys
	// in memiavl, some already migrated to flatKV).
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// --- Phase 3: finish migration ---
	// 100 more blocks * 100 batch = 10,000 capacity vs. ~5,400 keys still to
	// drain (4,000 left over from phase 2 + ~1,400 new keys added during
	// phases 2+3). Comfortable margin to converge.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		100, // blocks to simulate
	)

	// --- Verification ---

	// All oracle data must be reachable through the migration router.
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// Count bank vs non-bank keys in the oracle.
	var bankKeyCount, nonBankKeyCount int64
	for _, kp := range keysInUse.keys {
		if kp.store == keys.BankStoreKey {
			bankKeyCount++
		} else {
			nonBankKeyCount++
		}
	}

	// Key count check.
	// flatKV holds every non-bank key + exactly 1 migration metadata key
	// (MigrationVersionKey). MigrationBoundaryKey is deleted on the final
	// migration block, leaving only the version key.
	// memiavl holds only bank keys; its MigrationStore tree is empty
	// (version written to flatKV, boundary deleted).
	require.Equal(t, nonBankKeyCount+1, GetFlatKVKeyCount(t, flatKVDB),
		"flatKV should hold every non-bank key plus the migration version key")
	require.Equal(t, bankKeyCount, GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl should hold only bank keys")

	// Migration version check.
	flatKVVersion, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.True(t, found, "migration version key must be present in flatKV after migration")
	require.Equal(t, uint64(Version2_MigrateAllButBank), flatKVVersion,
		"flatKV migration version should be Version2_MigrateAllButBank")

	// Migration boundary check. The boundary key tracks the in-progress
	// migration cursor. On the final migration block it is deleted in the
	// same atomic write that records the new version, so post-completion it
	// must be absent from flatkv (memiavl never holds migration metadata).
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found,
		"migration boundary key must be cleared from flatKV after migration completes")

	// Placement check. Build a flatKV-store map containing every module
	// except bank — i.e. every store whose keys must end up in flatKV.
	flatKVStores := make(map[string]bool, len(keys.MemIAVLStoreKeys))
	for _, s := range keys.MemIAVLStoreKeys {
		if s != keys.BankStoreKey {
			flatKVStores[s] = true
		}
	}
	inMemoryRouter.VerifyKeyPlacement(t, memiavlDB, flatKVDB, flatKVStores)

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the MigrateBank data migration. At the start of this migration, every
// module except bank/ already lives in flatKV, and bank/ lives in memiavl
// (i.e. the AllMigratedButBank steady state). At the end, every module
// lives in flatKV; memiavl is empty.
//
// This test evaluates the 2->3 migration path. The setup phase relies on the
// AllMigratedButBank router (verified by TestAllMigratedButBank) to lay down
// a realistic v2 schema, then explicitly seeds flatKV's MigrationVersionKey
// since the AllMigratedButBank router does not itself write that bookkeeping.
func TestMigrateBank(t *testing.T) {

	rng := testutil.NewTestRandom()

	// Reserve stable directories so we can close and reopen the stores
	// mid-migration to simulate a process restart.
	memiavlDir := t.TempDir()
	flatKVDir := t.TempDir()

	// Migration metadata lives exclusively on flatkv; memiavl is opened
	// with just the production module stores. SimulateBlocks is
	// restricted to MemIAVLStoreKeys so no user data lands elsewhere.
	memiavlDB := NewTestMemIAVLCommitStore(t, memiavlDir, keys.MemIAVLStoreKeys)
	flatKVDB := NewTestFlatKVCommitStore(t, flatKVDir)

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// --- Phase 1: AllMigratedButBank setup ---
	// Lay down v2 state: bank/ in memiavl, everything else in flatKV. Drives
	// roughly equal load across all real modules so bank/ accumulates enough
	// keys to make the v2->v3 migration meaningful.
	allMigratedButBankRouter, err := BuildRouter(t.Context(), types.AllMigratedButBank, memiavlDB, flatKVDB, 0)
	require.NoError(t, err, "build AllMigratedButBank router")
	SimulateBlocks(t,
		NewTestMultiRouter(t, allMigratedButBankRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		100, // new keys per block
		100, // blocks to simulate
	)

	// The AllMigratedButBank router has no route for MigrationStore, so it
	// never writes the migration version key. A real chain at v2 would have
	// that key already (left behind by the prior MigrateAllButBank run);
	// seed it directly so the upcoming MigrateBank constructor can read v2
	// from the new DB instead of erroring out.
	SeedMigrationVersionInFlatKV(t, flatKVDB, Version2_MigrateAllButBank)

	// --- Phase 2: MigrateBank ---
	migrationRouter, err := BuildRouter(t.Context(), types.MigrateBank, memiavlDB, flatKVDB, 100)
	require.NoError(t, err, "build MigrateBank router")

	// Drive 2 blocks through the migration router. Phase 1 produced ~500
	// bank keys (1 of 20 modules at 100 new keys/block * 100 blocks); with
	// a batch size of 100 the migration drains those source keys in 5
	// blocks, so 2 blocks is deliberately short to leave the migration in
	// flight at the restart point: ~200 of the ~500 source bank keys
	// migrated to flatKV, ~300 still un-migrated in memiavl.
	// AssertMigrationInFlight below verifies this split before we close
	// the DBs.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		2,   // blocks to simulate
	)

	// Sanity check: the test must actually catch the migration in flight,
	// otherwise the restart below is degenerate (no boundary to resume from).
	inMemoryRouter.AssertMigrationInFlight(t, memiavlDB, flatKVDB, keys.BankStoreKey)

	// --- Restart ---
	// Close and reopen both backends. SimulateBlocks committed after every
	// block, so the on-disk state is consistent. The reopened router must
	// recover the migration manager's state from disk metadata - the
	// boundary key (migration cursor) and the source version stored in
	// flatKV.
	require.NoError(t, memiavlDB.Close(), "close memiavl before restart")
	require.NoError(t, flatKVDB.Close(), "close flatKV before restart")

	memiavlDB = NewTestMemIAVLCommitStore(t, memiavlDir, keys.MemIAVLStoreKeys)
	flatKVDB = NewTestFlatKVCommitStore(t, flatKVDir)

	commitBoth = func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	migrationRouter, err = BuildRouter(t.Context(), types.MigrateBank, memiavlDB, flatKVDB, 100)
	require.NoError(t, err, "rebuild MigrateBank router after restart")

	// Sanity check: all oracle data is still reachable through the rebuilt
	// router. Exercises the post-restart hybrid read path: ~200 bank keys
	// already in flatKV, ~300 still in memiavl awaiting migration, and
	// every other module (already in flatKV from the v2 setup) routed
	// directly to flatKV.
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// --- Phase 3: finish migration ---
	// 100 more blocks after the restart. The first ~3 blocks finish draining
	// the ~300 un-migrated source bank keys (batch 100); the remaining ~97
	// blocks run in passthrough mode and exercise normal user-key churn
	// against the post-completion write path.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		100, // blocks to simulate
	)

	// --- Verification ---

	// All oracle data must be reachable through the migration router.
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// After v3 every key in the oracle lives in flatKV.
	totalKeyCount := int64(keysInUse.Len())

	// Key count check.
	// flatKV holds every key + exactly 1 migration metadata key
	// (MigrationVersionKey). MigrationBoundaryKey is deleted on the final
	// migration block, leaving only the version key.
	// memiavl is empty: the bank tree was drained by the migration, no
	// other tree ever held user data, and the MigrationStore tree never
	// received the version key (it is written exclusively to flatKV).
	require.Equal(t, totalKeyCount+1, GetFlatKVKeyCount(t, flatKVDB),
		"flatKV should hold every key plus the migration version key")
	require.Equal(t, int64(0), GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl should be empty after migration")

	// Migration version check.
	flatKVVersion, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.True(t, found, "migration version key must be present in flatKV after migration")
	require.Equal(t, uint64(Version3_FlatKVOnly), flatKVVersion,
		"flatKV migration version should be Version3_FlatKVOnly")

	// Migration boundary check. The boundary key tracks the in-progress
	// migration cursor. On the final migration block it is deleted in the
	// same atomic write that records the new version, so post-completion it
	// must be absent from flatkv (memiavl never holds migration metadata).
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found,
		"migration boundary key must be cleared from flatKV after migration completes")

	// Placement check. After v3, every module's keys must be in flatKV and
	// absent from memiavl, including bank/.
	flatKVStores := make(map[string]bool, len(keys.MemIAVLStoreKeys))
	for _, s := range keys.MemIAVLStoreKeys {
		flatKVStores[s] = true
	}
	inMemoryRouter.VerifyKeyPlacement(t, memiavlDB, flatKVDB, flatKVStores)

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}
