package migration

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/testutil"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// TestBasisCase exercises the test framework itself end-to-end against raw
// memiavl + flatKV stores. No production router is involved: every
// changeset is fanned out to all three backends in lockstep via the
// multiRouter, then post-run state is verified for oracle equivalence and
// matching key counts. A regression here points at the framework or the
// raw stores, not at any migration logic.
func TestBasisCase(t *testing.T) {

	rng := testutil.NewTestRandom()

	// Real memiavl backend with a passthrough test router that forwards
	// reads and writes verbatim to it, performing no routing of its own.
	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), keys.MemIAVLStoreKeys)
	memiavlRouter := NewTestMemIAVLRouter(t, memiavlDB)

	// Real flatKV backend with a similarly passthrough test router.
	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())
	flatKVRouter := NewTestFlatKVRouter(t, flatKVDB)

	// Oracle: an in-memory map keyed by (store, key). It is the source of
	// truth that the verification phase compares the real backends against.
	inMemoryRouter := NewTestInMemoryRouter()

	keysInUse := newLiveKeySet()

	// Tees every ApplyChangeSets call to all three backends so they
	// accumulate identical state. Reads go through every backend and the
	// multiRouter errors if any disagree, providing a per-read consistency
	// check on top of the post-run verification.
	multiRouter := NewTestMultiRouter(t, inMemoryRouter, memiavlRouter, flatKVRouter)

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit(flatKVDB.Version() + 1)
		require.NoError(t, err, "flatKV commit")
	}

	// Drive a mixed insert / update / delete / read workload across every
	// production module store, fanning every write to memiavl, flatKV, and
	// the oracle simultaneously.
	SimulateBlocks(t,
		multiRouter,
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
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

// Test the MemiavlOnly steady-state router. This is the pre-migration version 0
// schema: every module routes to memiavl and there is no migration manager (or
// flatKV) in the data path. Bootstrap will pass nil for flatKV in this mode in
// production, and this test does the same. MigrationStore is intentionally not
// mounted on memiavl because V0 nodes will be deployed before the migration
// store is introduced; a realistic V0 layout has no MigrationStore tree.
func TestMemiavlOnly(t *testing.T) {

	rng := testutil.NewTestRandom()

	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), keys.MemIAVLStoreKeys)

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	memiavlOnlyRouter, err := BuildRouter(t.Context(), types.MemiavlOnly, memiavlDB, nil, 0)
	require.NoError(t, err)

	commit := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
	}

	SimulateBlocks(t,
		NewTestMultiRouter(t, memiavlOnlyRouter, inMemoryRouter),
		commit,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		200, // new keys per block
		100, // blocks to simulate
	)

	// Read path correctness: every oracle key is reachable through the router.
	inMemoryRouter.VerifyContainsSameData(t, memiavlOnlyRouter)

	// Exact key count check. The oracle's logical key count must equal the
	// total physical key count across every memiavl tree. This doubles as the
	// "no migration bookkeeping written" check: any spurious write to an
	// internal store would inflate the count and fail this assertion.
	require.Equal(t, int64(keysInUse.Len()), GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl should contain exactly the simulated keys with no phantom rows")

	require.NoError(t, memiavlDB.Close(), "close memiavl")
}

// Test the EVMMigrated steady-state router. This is the post-MigrateEVM
// migration version 1 schema: EVM data lives entirely in flatKV, every
// other module lives entirely in memIAVL, and there is no migration
// manager in the data path. Because the schema is stable, a single
// long simulation is sufficient — there is no in-flight migration to
// resume across a restart.
func TestEVMMigrated(t *testing.T) {

	rng := testutil.NewTestRandom()

	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), keys.MemIAVLStoreKeys)
	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	evmMigratedRouter, err := BuildRouter(t.Context(), types.EVMMigrated, memiavlDB, flatKVDB, 0)
	require.NoError(t, err)

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit(flatKVDB.Version() + 1)
		require.NoError(t, err, "flatKV commit")
	}

	SimulateBlocks(t,
		NewTestMultiRouter(t, evmMigratedRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
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
		if kp.store == keys.EVMStoreKey {
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
		map[string]bool{keys.EVMStoreKey: true},
	)

	// The steady-state router must not write any migration metadata to
	// either backend — that is the responsibility of the migration manager,
	// which is not present in this data path.
	_, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.False(t, found, "EVMMigrated router must not write a migration version to flatKV")
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found, "EVMMigrated router must not write a migration boundary to flatKV")

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the AllMigratedButBank steady-state router. This is the
// post-MigrateAllButBank migration version 2 schema: every module except
// bank/ lives in flatKV, bank/ lives in memiavl, and there is no migration
// manager in the data path. Because the schema is stable, a single long
// simulation is sufficient — there is no in-flight migration to resume
// across a restart.
func TestAllMigratedButBank(t *testing.T) {

	rng := testutil.NewTestRandom()

	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), keys.MemIAVLStoreKeys)
	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	allMigratedButBankRouter, err := BuildRouter(t.Context(), types.AllMigratedButBank, memiavlDB, flatKVDB, 0)
	require.NoError(t, err)

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit(flatKVDB.Version() + 1)
		require.NoError(t, err, "flatKV commit")
	}

	SimulateBlocks(t,
		NewTestMultiRouter(t, allMigratedButBankRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		200, // new keys per block
		100, // blocks to simulate
	)

	// Read path correctness: every oracle key is reachable through the router.
	inMemoryRouter.VerifyContainsSameData(t, allMigratedButBankRouter)

	// Count bank vs non-bank keys in the oracle.
	var bankKeyCount, nonBankKeyCount int64
	for _, kp := range keysInUse.keys {
		if kp.store == keys.BankStoreKey {
			bankKeyCount++
		} else {
			nonBankKeyCount++
		}
	}

	// Key count check. Unlike TestMigrateAllButBank, there is no migration
	// manager in this router, so flatKV holds exactly the non-bank keys
	// (no version / boundary metadata) and memiavl holds exactly the bank
	// keys.
	require.Equal(t, nonBankKeyCount, GetFlatKVKeyCount(t, flatKVDB),
		"flatKV should contain only non-bank keys in steady state")
	require.Equal(t, bankKeyCount, GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl should contain only bank keys in steady state")

	// Placement check. Build a flatKV-store map containing every module
	// except bank — i.e. every store whose keys must end up in flatKV.
	flatKVStores := make(map[string]bool, len(keys.MemIAVLStoreKeys))
	for _, s := range keys.MemIAVLStoreKeys {
		if s != keys.BankStoreKey {
			flatKVStores[s] = true
		}
	}
	inMemoryRouter.VerifyKeyPlacement(t, memiavlDB, flatKVDB, flatKVStores)

	// The steady-state router must not write any migration metadata to
	// either backend — that is the responsibility of the migration manager,
	// which is not present in this data path.
	_, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.False(t, found, "AllMigratedButBank router must not write a migration version to flatKV")
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found, "AllMigratedButBank router must not write a migration boundary to flatKV")

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the FlatKVOnly steady-state router. This is the post-MigrateBank
// terminal version 3 schema: every module routes to flatKV and there is no
// migration manager (or memiavl) in the data path. Bootstrap will pass nil
// for memiavl in this mode in production, and this test does the same.
func TestFlatKVOnly(t *testing.T) {

	rng := testutil.NewTestRandom()

	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	flatKVOnlyRouter, err := BuildRouter(t.Context(), types.FlatKVOnly, nil, flatKVDB, 0)
	require.NoError(t, err)

	commit := func() {
		_, err := flatKVDB.Commit(flatKVDB.Version() + 1)
		require.NoError(t, err, "flatKV commit")
	}

	SimulateBlocks(t,
		NewTestMultiRouter(t, flatKVOnlyRouter, inMemoryRouter),
		commit,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		200, // new keys per block
		100, // blocks to simulate
	)

	// Read path correctness: every oracle key is reachable through the router.
	inMemoryRouter.VerifyContainsSameData(t, flatKVOnlyRouter)

	// Exact key count check. The oracle's logical key count must equal the
	// physical row count in flatKV. With random 20-byte EVM addresses, the
	// nonce/codehash account-row merging in flatKV does not collapse rows
	// (collisions are astronomically unlikely), so logical and physical
	// counts agree — same assumption TestBasisCase relies on.
	require.Equal(t, int64(keysInUse.Len()), GetFlatKVKeyCount(t, flatKVDB),
		"flatKV should contain exactly the simulated keys with no phantom rows")

	// The terminal-state router must not write any migration metadata to
	// flatKV — that is the responsibility of the migration manager, which
	// is not present in this data path.
	_, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.False(t, found, "FlatKVOnly router must not write a migration version to flatKV")
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found, "FlatKVOnly router must not write a migration boundary to flatKV")

	require.NoError(t, flatKVDB.Close(), "close flatKV")
}

// Test the TestOnlyDualWrite router. Every module routes to memiavl;
// evm/ traffic is additionally fanned out to flatKV. There is no
// migration manager in the data path. This mode is for test clusters
// that need EVM data accessible via both memiavl reads and FlatKV reads
// — it must not be deployed to production but must remain supported for
// parity with the existing composite-store tests.
//
// Invariant pinned by this test:
//   - memiavl holds every key (evm + non-evm)
//   - flatKV holds exactly the evm keys
//   - reads through the router come from memiavl (the primary)
//   - no migration metadata is written to either backend
func TestDualWrite(t *testing.T) {

	rng := testutil.NewTestRandom()

	memiavlDB := NewTestMemIAVLCommitStore(t, t.TempDir(), keys.MemIAVLStoreKeys)
	flatKVDB := NewTestFlatKVCommitStore(t, t.TempDir())

	inMemoryRouter := NewTestInMemoryRouter()
	keysInUse := newLiveKeySet()

	dualWriteRouter, err := BuildRouter(t.Context(), types.TestOnlyDualWrite, memiavlDB, flatKVDB, 0)
	require.NoError(t, err)

	commitBoth := func() {
		_, err := memiavlDB.Commit()
		require.NoError(t, err, "memiavl commit")
		_, err = flatKVDB.Commit(flatKVDB.Version() + 1)
		require.NoError(t, err, "flatKV commit")
	}

	SimulateBlocks(t,
		NewTestMultiRouter(t, dualWriteRouter, inMemoryRouter),
		commitBoth,
		rng,
		keysInUse,
		keys.MemIAVLStoreKeys,
		100, // reads per block
		100, // updates per block
		20,  // deletes per block
		200, // new keys per block
		100, // blocks to simulate
	)

	// Read path correctness: every oracle key is reachable through the
	// router. Reads come from memiavl (the primary), which under the
	// dual-write invariant holds every key, so this passes for both
	// evm and non-evm modules.
	inMemoryRouter.VerifyContainsSameData(t, dualWriteRouter)

	// Count EVM vs non-EVM keys in the oracle.
	var evmKeyCount, nonEVMKeyCount int64
	for _, kp := range keysInUse.keys {
		if kp.store == keys.EVMStoreKey {
			evmKeyCount++
		} else {
			nonEVMKeyCount++
		}
	}

	// Key count check. Unlike the steady-state routers, dual-write
	// keeps every key in memiavl and additionally mirrors evm keys
	// into flatKV. No migration metadata is written, so flatKV's
	// physical key count equals exactly the evm logical key count
	// (same physical-vs-logical caveat as TestBasisCase / TestFlatKVOnly:
	// random 20-byte EVM addresses make collapsing-row collisions
	// astronomically unlikely).
	require.Equal(t, evmKeyCount+nonEVMKeyCount, GetMemIAVLKeyCount(t, memiavlDB),
		"memiavl must hold every key (evm + non-evm) under dual-write")
	require.Equal(t, evmKeyCount, GetFlatKVKeyCount(t, flatKVDB),
		"flatKV must hold exactly the dual-written evm keys")

	// Per-key dual-write invariant: every oracle key is in memiavl,
	// and present in flatKV iff its store is keys.EVMStoreKey.
	// VerifyKeyPlacement assumes mutually-exclusive placement and so
	// can't be used here; assert directly.
	memIAVLGet := func(store string, key []byte) ([]byte, bool) {
		childStore := memiavlDB.GetChildStoreByName(store)
		if childStore == nil {
			return nil, false
		}
		v := childStore.Get(key)
		return v, v != nil
	}
	for _, kp := range keysInUse.keys {
		key := []byte(kp.key)
		expected, _, err := inMemoryRouter.Read(kp.store, key)
		require.NoError(t, err)

		got, ok := memIAVLGet(kp.store, key)
		require.True(t, ok, "store %q key %x must be in memiavl under dual-write", kp.store, key)
		require.Equal(t, expected, got, "store %q key %x value mismatch in memiavl", kp.store, key)

		flatVal, flatFound := flatKVDB.Get(kp.store, key)
		if kp.store == keys.EVMStoreKey {
			require.True(t, flatFound,
				"evm store key %x must be mirrored to flatKV under dual-write", key)
			require.Equal(t, expected, flatVal,
				"evm store key %x value mismatch in flatKV", key)
		} else {
			require.False(t, flatFound,
				"non-evm store %q key %x must not appear in flatKV under dual-write", kp.store, key)
		}
	}

	// The dual-write router must not write any migration metadata to
	// either backend — that is the responsibility of the migration
	// manager, which is not present in this data path.
	_, found := ReadMigrationVersionFromFlatKV(t, flatKVDB)
	require.False(t, found, "TestOnlyDualWrite router must not write a migration version to flatKV")
	_, found = ReadMigrationBoundaryFromFlatKV(t, flatKVDB)
	require.False(t, found, "TestOnlyDualWrite router must not write a migration boundary to flatKV")

	require.NoError(t, memiavlDB.Close(), "close memiavl")
	require.NoError(t, flatKVDB.Close(), "close flatKV")
}
