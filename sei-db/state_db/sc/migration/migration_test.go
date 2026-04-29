package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Do operations on regular flatKV and memIAVL databases to verify that the test framework is sane.
func TestBasisCase(t *testing.T) {

	rng := newSeededTestRandom(t)

	// Write the data to memiavl.
	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), MemIAVLStoreKeys)
	memiavlRouter := NewTestMemIAVLRouter(t, memiavlDB)

	// Write the data to flatkv.
	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())
	flatKVRouter := NewTestFlatKVRouter(t, flatKVDB)

	// An in-memory store of data, used to compare the data in the other two stores.
	inMemoryRouter := NewTestInMemoryRouter()

	keysInUse := newLiveKeySet()

	multiRouter := NewTestMultiRouter(t, inMemoryRouter, memiavlRouter, flatKVRouter)

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// Throw a bunch of data at the stores.
	SimulateBlocks(t,
		multiRouter,
		commitBoth,
		rng,
		keysInUse,
		MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		200, // new keys per block
		100, // blocks to simulate
	)

	// Verify that both backends contain all the data the oracle knows about.
	inMemoryRouter.VerifyContainsSameData(t, memiavlRouter)
	inMemoryRouter.VerifyContainsSameData(t, flatKVRouter)

	// Key count check: the oracle knows the exact number of live logical keys.
	// Both backends must contain exactly that many keys. This rules out any
	// phantom keys (extra rows) that VerifyContainsSameData cannot detect.
	expectedKeyCount := int64(keysInUse.Len())
	require.Equal(t, expectedKeyCount, GetMemIAVLKeyCount(t, memiavlDB), "memiavl key count")
	require.Equal(t, expectedKeyCount, GetFlatKVKeyCount(t, flatKVDB), "flatkv key count")

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the MigrateEVM data migration. At the start of this migration, all data lives in memIAVL.
// At the end of this migration, all evm/ data lives in flatkv, and all other data remains in memIAVL.
//
// This test evaluates the 0->1 migration path.
func TestMigrateEVM(t *testing.T) {

	rng := newSeededTestRandom(t)

	// Reserve stable directories so we can close and reopen the stores
	// mid-migration to simulate a process restart.
	memiavlDir := t.TempDir()
	flatKVDir := t.TempDir()

	memiavlStores := append(MemIAVLStoreKeys, MigrationStore) //nolint:gocritic

	// All data is initially in memiavl. MigrationStore is included so the
	// migration manager can read/write its version and boundary metadata there,
	// but it is intentionally excluded from SimulateBlocks so no user data
	// lands in it.
	memiavlDB := NewTestMemIAVLCommitStore(t, memiavlDir, memiavlStores)
	memiavlRouter := NewTestMemIAVLRouter(t, memiavlDB)

	// All evm/ data will be migrated to flatkv.
	flatKVDB := NewTestFlatKVCommitStore(t, flatKVDir)

	// An in-memory store of data, used to compare the data in the other two stores.
	inMemoryRouter := NewTestInMemoryRouter()

	keysInUse := newLiveKeySet()

	// commitBoth closes over the current memiavlDB / flatKVDB pointers. After
	// the restart below those variables are reassigned, so we re-bind it again
	// at that point.
	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// Step 1: write a bunch of data to the memiavl database. There will be no data sent to flatkv.
	SimulateBlocks(t,
		NewTestMultiRouter(t, memiavlRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		100, // new keys per block
		100, // blocks to simulate
	)

	// Build a migration router that will migrate the evm/ data to flatkv.
	migrationRouter, err := BuildRouter(t.Context(), MigrateEVM, memiavlDB, flatKVDB, 100)
	require.NoError(t, err)

	// Step 2: migrate 50 blocks. There are ~100*100 keys, if we migrate 100 keys per block, we should migrate some
	// but not all of the data.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		10,  // deletes per block
		10,  // new keys per block
		50,  // blocks to simulate
	)

	// Close and restart flatKV and memiavl mid-migration. We want to verify
	// that the in-progress migration is not corrupted by a restart and that
	// it resumes from the persisted boundary. SimulateBlocks already committed
	// after every block, so closing here is safe.
	require.NoError(t, memiavlDB.Close(), "close memiavl before restart")
	require.NoError(t, flatKVDB.Close(), "close flatKV before restart")

	memiavlDB = NewTestMemIAVLCommitStore(t, memiavlDir, memiavlStores)
	memiavlRouter = NewTestMemIAVLRouter(t, memiavlDB)
	flatKVDB = NewTestFlatKVCommitStore(t, flatKVDir)

	// Rebuild commitBoth so it points at the freshly-reopened stores.
	commitBoth = func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	// Rebuild the migration router on top of the freshly-reopened DBs. The
	// migration manager reads its boundary and version from disk, so it picks
	// up exactly where it left off.
	migrationRouter, err = BuildRouter(t.Context(), MigrateEVM, memiavlDB, flatKVDB, 100)
	require.NoError(t, err, "rebuild migration router after restart")

	// Sanity check: all oracle data is still reachable through the rebuilt router.
	inMemoryRouter.VerifyContainsSameData(t, migrationRouter)

	// Step 3: finish the migration. 100 more blocks @ 100 keys per blockshould be enough to migrate the
	// remaining data. We do insert 10 keys per block, but even with perfect worst case key placement,
	// 150 blocks total should be enough.
	SimulateBlocks(t,
		NewTestMultiRouter(t, migrationRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		MemIAVLStoreKeys,
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
		if kp.store == EVMStoreKey {
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
	_, found = ReadMigrationVersionFromMemIAVL(t, memiavlDB)
	require.False(t, found,
		"migration version key must not be present in memiavl (it is written exclusively to flatKV)")

	// Migration boundary check. The boundary key tracks the in-progress
	// migration cursor. On the final migration block it is deleted in the same
	// atomic write that records the new version, so post-completion it must be
	// absent from both backends.
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found,
		"migration boundary key must be cleared from flatKV after migration completes")
	_, found = ReadMigrationBoundaryFromMemIAVL(t, memiavlDB)
	require.False(t, found,
		"migration boundary key must not be present in memiavl")

	// Placement check: each oracle key must be in the correct backend and absent from the other.
	inMemoryRouter.VerifyKeyPlacement(t, memiavlDB, flatKVDB,
		map[string]bool{EVMStoreKey: true},
	)

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the EVMMigrated steady-state router. This is the post-MigrateEVM
// migration version 1 schema: EVM data lives entirely in flatKV, every
// other module lives entirely in memIAVL, and there is no migration
// manager in the data path. Because the schema is stable, a single
// long simulation is sufficient — there is no in-flight migration to
// resume across a restart.
func TestEVMMigrated(t *testing.T) {

	rng := newSeededTestRandom(t)

	// Include MigrationStore in memiavl so ReadMigrationVersion/Boundary
	// can probe it without hitting "store not found"; the EVMMigrated
	// router itself never touches MigrationStore, so the tree stays empty.
	memiavlStores := append(MemIAVLStoreKeys, MigrationStore) //nolint:gocritic
	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), memiavlStores)
	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	evmMigratedRouter, err := BuildRouter(t.Context(), EVMMigrated, memiavlDB, flatKVDB, 0)
	require.NoError(t, err)

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit()
		require.NoError(t, err, "flatKV commit")
	}

	SimulateBlocks(t,
		NewTestMultiRouter(t, evmMigratedRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		200, // new keys per block
		100, // blocks to simulate
	)

	// Read path correctness: every oracle key is reachable through the router.
	inMemoryRouter.VerifyContainsSameData(t, evmMigratedRouter)

	// Count EVM vs non-EVM keys in the oracle.
	var evmKeyCount, nonEVMKeyCount int64
	for _, kp := range keysInUse.keys {
		if kp.store == EVMStoreKey {
			evmKeyCount++
		} else {
			nonEVMKeyCount++
		}
	}

	// Key count check. Unlike TestMigrateEVM, there is no migration manager
	// in this router, so flatKV holds exactly the EVM keys (no version /
	// boundary metadata) and memiavl holds exactly the non-EVM keys.
	require.Equal(t, evmKeyCount, GetFlatKVKeyCount(t, flatKVDB),
		"flatKV should contain only EVM keys in steady state")
	require.Equal(t, nonEVMKeyCount, GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl should contain only non-EVM keys in steady state")

	// Placement check: each oracle key must be in the correct backend and absent from the other.
	inMemoryRouter.VerifyKeyPlacement(t, memiavlDB, flatKVDB,
		map[string]bool{EVMStoreKey: true},
	)

	// The steady-state router must not write any migration metadata to
	// either backend — that is the responsibility of the migration manager,
	// which is not present in this data path.
	_, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.False(t, found, "EVMMigrated router must not write a migration version to flatKV")
	_, found = ReadMigrationVersionFromMemIAVL(t, memiavlDB)
	require.False(t, found, "EVMMigrated router must not write a migration version to memiavl")
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found, "EVMMigrated router must not write a migration boundary to flatKV")
	_, found = ReadMigrationBoundaryFromMemIAVL(t, memiavlDB)
	require.False(t, found, "EVMMigrated router must not write a migration boundary to memiavl")

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}
