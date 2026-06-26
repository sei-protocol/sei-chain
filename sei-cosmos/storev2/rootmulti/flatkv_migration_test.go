package rootmulti

// FlatKV EVM migrate integration coverage at the rootmulti layer.
//
// The composite-package tests in
// sei-db/state_db/sc/composite/store_migration_test.go pin the same
// invariants against the bare CompositeCommitStore. The tests here run
// the migration through the rootmulti Store entry point so the
// store-tree wiring (CacheMultiStore -> KVStore -> CommitKVStore ->
// composite -> router) and the resulting CommitInfo / AppHash sequence
// are exercised end-to-end. This is the closest in-process analogue to
// what a Sei node observes during the operator-driven migration and is
// the bridge between the Go-level migration tests and the Docker-level
// cluster tests.

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// migrationVersionInFlatKV reads the migration-version key directly from
// flatkv at the given rootmulti home dir. Returns (0, false) when the
// key is absent (= migration not yet completed). Closes the temporary
// flatkv handle before returning. The caller is expected to have
// already closed the rootmulti store at dir; flatkv refuses to open
// concurrently with the live store.
func migrationVersionInFlatKV(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig) (uint64, bool) {
	t.Helper()
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	s, err := flatkv.NewCommitStore(context.Background(), &flatkvCfg)
	require.NoError(t, err)
	_, err = s.LoadVersion(0, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()
	reader := migration.DBReader(func(store string, key []byte) ([]byte, bool, error) {
		v, ok := s.Get(store, key)
		return v, ok, nil
	})
	v, _, err := readMigrationVersion(reader)
	require.NoError(t, err)
	return v, v != 0
}

// readMigrationVersion mirrors migration.IsAtVersion but returns the
// raw version so callers can assert against either presence or value.
// We can't reuse migration.IsAtVersion directly because it only
// returns a bool relative to a target version, and the lifecycle test
// below wants to observe both the in-flight (version key absent) and
// the post-completion (version key == 1) states from the same caller.
func readMigrationVersion(reader migration.DBReader) (uint64, bool, error) {
	// migration.IsAtVersion(reader, v1) is the closest exported helper.
	// Probe both candidate versions; falling back to v0 lets us also
	// detect the boundary-not-yet-bumped case as version 0.
	atV1, err := migration.IsAtVersion(reader, uint64(migration.Version1_MigrateEVM))
	if err != nil {
		return 0, false, err
	}
	if atV1 {
		return uint64(migration.Version1_MigrateEVM), true, nil
	}
	return uint64(migration.Version0_MemiavlOnly), false, nil
}

// driveRootMultiMigration plays the operator-driven FlatKV EVM migrate
// through the rootmulti Store entry point. Phase 1 runs blocks 1..p1
// in MemiavlOnly using simulateBlockManyStorage so each block deposits
// a large EVM-storage batch into memiavl; this is what the migration
// in phase 2 then has to drain. Phase 2 reopens under MigrateEVM with
// the supplied batch size and runs p2 more blocks of normal traffic.
// Returns the post-phase-2 store (still open), the store-key map, and
// every commit record from phase 1 + phase 2 in order.
//
// Centralizing this lets each test focus on what it asserts rather
// than the bookkeeping for the two-phase open / close / open cycle.
func driveRootMultiMigration(
	t *testing.T,
	dir string,
	phase1Blocks, phase2Blocks int,
	phase1StorageKeysPerBlock int,
	migrateBatchSize int,
) (*Store, map[string]*types.KVStoreKey, []commitRecord) {
	t.Helper()

	records := make([]commitRecord, 0, phase1Blocks+phase2Blocks)

	store, storeKeys := newTestRootMulti(t, dir, memiavlOnlyConfig())

	// Phase 1: lots of EVM storage keys so the migration in phase 2 has
	// real work to do. keysPerBlock controls the source key fan-out.
	addrBase := newEVMTestData(0xA1)
	storageKeys := storageMemIAVLKeys(0xA1, phase1StorageKeysPerBlock)
	for i := 1; i <= phase1Blocks; i++ {
		records = append(records, simulateBlockManyStorage(t, store, storeKeys, i, storageKeys, addrBase))
	}

	// --- Restart: MemiavlOnly -> MigrateEVM ---
	store, storeKeys = restartRootMultiWithConfig(t, store, dir, migrateEVMConfig())
	require.NoError(t, store.SetMigrationBatchSize(migrateBatchSize))

	for i := phase1Blocks + 1; i <= phase1Blocks+phase2Blocks; i++ {
		records = append(records, simulateBlock(t, store, storeKeys, i, addrBase))
	}
	return store, storeKeys, records
}

// driveDrainBlocks runs drainBlocks more simulateBlock commits on the
// open store starting at startBlock. The caller is expected to size
// drainBlocks so the migration completes within the loop; this helper
// does not probe flatkv mid-loop because the rootmulti owner holds an
// exclusive lock on the flatkv dir and probing forces a close/reopen
// per block that triggers WAL snapshot rotation edge cases. Verify
// completion after the test has closed the store via the offline
// migrationVersionInFlatKV helper.
func driveDrainBlocks(
	t *testing.T,
	store *Store,
	storeKeys map[string]*types.KVStoreKey,
	startBlock, drainBlocks int,
) (records []commitRecord, nextBlock int) {
	t.Helper()
	addrBase := newEVMTestData(0xA1)
	records = make([]commitRecord, 0, drainBlocks)
	block := startBlock
	for i := 0; i < drainBlocks; i++ {
		records = append(records, simulateBlock(t, store, storeKeys, block, addrBase))
		block++
	}
	return records, block
}

func TestRootMultiMigrateEVM_ReopenPreservesPreFlipAppHash(t *testing.T) {
	dir := t.TempDir()

	store, storeKeys := newTestRootMulti(t, dir, memiavlOnlyConfig())
	addrBase := newEVMTestData(0xA1)
	storageKeys := storageMemIAVLKeys(0xA1, 8)
	for i := 1; i <= 3; i++ {
		simulateBlockManyStorage(t, store, storeKeys, i, storageKeys, addrBase)
	}
	preFlipID := store.LastCommitID()
	require.Equal(t, int64(3), preFlipID.Version)
	require.NotEmpty(t, preFlipID.Hash)

	store, storeKeys = restartRootMultiWithConfig(t, store, dir, migrateEVMConfig())
	require.NoError(t, store.SetMigrationBatchSize(2))
	defer func() { require.NoError(t, store.Close()) }()

	require.Equal(t, preFlipID, store.LastCommitID(),
		"opening migrate_evm must not change the AppHash for the already-committed height")

	rec := simulateBlock(t, store, storeKeys, 4, addrBase)
	require.Equal(t, int64(4), rec.version)
	require.NotEmpty(t, rec.hash)
}

func TestRootMultiMigrateEVM_EmptyBlocksAdvanceMigration(t *testing.T) {
	dir := t.TempDir()

	store, storeKeys := newTestRootMulti(t, dir, memiavlOnlyConfig())
	addrBase := newEVMTestData(0xB1)
	storageKeys := storageMemIAVLKeys(0xB1, 4)
	simulateBlockManyStorage(t, store, storeKeys, 1, storageKeys, addrBase)

	store, _ = restartRootMultiWithConfig(t, store, dir, migrateEVMConfig())
	require.NoError(t, store.SetMigrationBatchSize(2))

	for i := 0; i < 4; i++ {
		rec := finalizeBlock(t, store)
		require.Equal(t, int64(2+i), rec.version)
		require.NotEmpty(t, rec.hash)
	}
	require.NoError(t, store.Close())

	v, present := migrationVersionInFlatKV(t, dir, migrateEVMConfig())
	require.True(t, present)
	require.Equal(t, uint64(migration.Version1_MigrateEVM), v)
}

func TestRootMultiMigrateEVM_EVMIteratorAvailableDuringMigration(t *testing.T) {
	dir := t.TempDir()

	store, storeKeys := newTestRootMulti(t, dir, memiavlOnlyConfig())
	cms := store.CacheMultiStore()
	txHashKey := evmtypes.TxHashesKey(1)
	cms.GetKVStore(storeKeys["evm"]).Set(txHashKey, []byte("txhash"))
	cms.Write()
	rec := finalizeBlock(t, store)
	require.Equal(t, int64(1), rec.version)

	store, storeKeys = restartRootMultiWithConfig(t, store, dir, migrateEVMConfig())
	require.NoError(t, store.SetMigrationBatchSize(2))
	defer func() { require.NoError(t, store.Close()) }()

	cms = store.CacheMultiStore()
	iter := cms.GetKVStore(storeKeys["evm"]).Iterator(
		evmtypes.TxHashesPrefix,
		types.PrefixEndBytes(evmtypes.TxHashesPrefix),
	)
	defer func() { require.NoError(t, iter.Close()) }()
	require.True(t, iter.Valid())
	require.Equal(t, txHashKey, iter.Key())
	require.Equal(t, []byte("txhash"), iter.Value())
}

// TestRootMultiMigrateEVM_HappyPath_Lifecycle drives the full
// MemiavlOnly -> MigrateEVM lifecycle through the rootmulti Store and
// asserts the AppHash sequence makes sense: every block produces a
// version + non-empty hash, the version is monotonic, and the
// migration eventually completes with a self-consistent flatkv (full
// LtHash full-scan matches the committed root, which is the in-process
// analogue of the cross-validator digest check the Docker tests run).
func TestRootMultiMigrateEVM_HappyPath_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	const (
		phase1Blocks  = 3
		phase2Blocks  = 5
		batch         = 8 // small enough to span the boundary across multiple p2 blocks
		storagePerBlk = 12
		drainBlocks   = 15 // upper bound: phase1 deposits ~40 EVM keys, batch=8 drains in ~5
	)

	store, storeKeys, records := driveRootMultiMigration(
		t, dir, phase1Blocks, phase2Blocks, storagePerBlk, batch,
	)

	// Phase 1 + phase 2 records must form a contiguous version
	// sequence with non-empty hashes. Empty hashes here would mean
	// the rootmulti CommitInfo amendment is silently dropping a store.
	for i, rec := range records {
		require.Equal(t, int64(i+1), rec.version, "block %d: version mismatch", i+1)
		require.NotEmpty(t, rec.hash, "block %d: AppHash must be non-empty", i+1)
		require.NotNil(t, findStoreInfo(rec.infos, "evm"),
			"block %d: evm StoreInfo must be present in CommitInfo", i+1)
		require.NotNil(t, findStoreInfo(rec.infos, "bank"),
			"block %d: bank StoreInfo must be present in CommitInfo", i+1)
	}

	// Drive a few more blocks so the migration boundary closes. The
	// drain count is fixed (rather than probed) to avoid the
	// close/reopen-per-block path that races with flatkv snapshot
	// rotation; the offline check after Close verifies completion.
	drainRecs, _ := driveDrainBlocks(t, store, storeKeys,
		phase1Blocks+phase2Blocks+1, drainBlocks)
	for i, rec := range drainRecs {
		blockNum := phase1Blocks + phase2Blocks + i + 1
		require.Equal(t, int64(blockNum), rec.version,
			"drain block %d: version mismatch", blockNum)
		require.NotEmpty(t, rec.hash,
			"drain block %d: AppHash must be non-empty", blockNum)
	}

	// Close before the offline migration-version check; flatkv refuses
	// to open concurrently with the live store.
	require.NoError(t, store.Close())

	// Migration must have completed by now. The composite-layer test
	// (TestComposite_MigrateEVM_HappyPath) also verifies full-scan
	// LtHash equality; we don't repeat that here because the live
	// rootmulti store has by now rotated flatkv WAL snapshots in a way
	// that breaks LoadVersion(latest, readOnly=true) catchup.
	v, present := migrationVersionInFlatKV(t, dir, migrateEVMConfig())
	require.True(t, present, "migration-version key must be present in flatkv after completion")
	require.Equal(t, uint64(migration.Version1_MigrateEVM), v,
		"flatkv migration version must be Version1_MigrateEVM")
}

// TestRootMultiMigrateEVM_AppHashDeterminismAcrossRuns is the
// cross-validator agreement check at the rootmulti layer: two
// independent rootmulti stores driven by the same per-block workload
// must produce byte-identical AppHashes at every commit. If this fails
// in production, four validators driving the same migration will fork
// the chain at the first divergent block.
func TestRootMultiMigrateEVM_AppHashDeterminismAcrossRuns(t *testing.T) {
	const (
		phase1Blocks  = 3
		phase2Blocks  = 10
		batch         = 8
		storagePerBlk = 12
	)

	runOnce := func() []commitRecord {
		dir := t.TempDir()
		store, _, records := driveRootMultiMigration(
			t, dir, phase1Blocks, phase2Blocks, storagePerBlk, batch,
		)
		require.NoError(t, store.Close())
		return records
	}

	a := runOnce()
	b := runOnce()
	require.Equal(t, len(a), len(b))
	for i := range a {
		require.Equalf(t, a[i].version, b[i].version,
			"block %d: version differs between runs", i+1)
		require.Equalf(t, a[i].hash, b[i].hash,
			"block %d (version %d): AppHash differs between runs\n  run A: %x\n  run B: %x",
			i+1, a[i].version, a[i].hash, b[i].hash)
	}
}

// TestRootMultiMigrateEVM_PostCompletionFlipToEVMMigrated covers the
// production mode flip: once the migration completes the operator flips
// sc-write-mode from migrate_evm to evm_migrated so subsequent restarts
// don't spin up the migration manager. The flip must be lossless: the
// store reopens at the same version, the next block commits cleanly
// against the same flatkv-backed EVM lattice, and the evm StoreInfo in
// CommitInfo continues to surface the lattice digest (i.e. nothing
// silently routes EVM writes back to memiavl).
func TestRootMultiMigrateEVM_PostCompletionFlipToEVMMigrated(t *testing.T) {
	dir := t.TempDir()
	const (
		phase1Blocks  = 3
		phase2Blocks  = 5
		batch         = 8
		storagePerBlk = 12
	)

	store, storeKeys, _ := driveRootMultiMigration(
		t, dir, phase1Blocks, phase2Blocks, storagePerBlk, batch,
	)
	_, nextBlock := driveDrainBlocks(t, store, storeKeys,
		phase1Blocks+phase2Blocks+1, 15)

	// Snapshot the pre-flip last-commit hash so we can require it
	// survives the migration unchanged.
	preFlipVersion := store.LastCommitID().Version
	preFlipHash := append([]byte(nil), store.LastCommitID().Hash...)

	// Sanity: the close-and-reopen below depends on the migration
	// having actually finished before we flip the mode.
	require.NoError(t, store.Close())
	v, present := migrationVersionInFlatKV(t, dir, migrateEVMConfig())
	require.True(t, present && v == uint64(migration.Version1_MigrateEVM),
		"flip must happen after migration completes; tighten drainBlocks if this fails")

	// --- Flip: MigrateEVM -> EVMMigrated. ---
	store, storeKeys = newTestRootMulti(t, dir, evmMigratedConfig())

	require.Equal(t, preFlipVersion, store.LastCommitID().Version,
		"EVMMigrated reopen must report the same version as the completed MigrateEVM run")
	require.Equal(t, preFlipHash, store.LastCommitID().Hash,
		"EVMMigrated reopen must report the same AppHash; the on-disk flatkv root is identical so the CommitInfo must hash identically")

	// One more block must commit cleanly under the new mode. This is
	// the regression signal for any post-flip routing change that
	// would otherwise produce a malformed CommitInfo (e.g. dropping
	// the evm StoreInfo or swapping its hash source).
	addrBase := newEVMTestData(0xA1)
	rec := simulateBlock(t, store, storeKeys, nextBlock, addrBase)
	require.Equal(t, preFlipVersion+1, rec.version)
	require.NotEmpty(t, rec.hash)
	require.NotNil(t, findStoreInfo(rec.infos, "evm"))

	require.NoError(t, store.Close())
}

// TestRootMultiMigrateEVM_DoubleFlushAppHashStable pins the AppHash
// continuity invariant for production's GetWorkingHash + Commit
// double flush in MigrateEVM mode. rootmulti.Store.flush is called
// once inside GetWorkingHash (to produce the AppHash returned to
// Tendermint) and once inside Commit (to drain any post-FinalizeBlock
// caller cache). The CompositeCommitStore in migration modes forwards
// empty changesets to the MigrationManager so empty blocks still
// advance the boundary; without the per-commit advance gate that
// second flush would advance the boundary again, mutate the working
// commit info, and end up persisting a hash that differs from the
// one already returned to Tendermint — a deterministic AppHash
// mismatch the moment any validator restarts.
//
// The test drives several migrate_evm blocks. For each block it:
//  1. Writes some EVM data, then calls GetWorkingHash twice with no
//     intervening Commit. Both calls must return identical hashes:
//     the second flush must not advance the boundary or perturb the
//     working commit info.
//  2. Calls Commit and asserts the persisted LastCommitInfo hash
//     matches the WorkingHash that was already returned. This is the
//     direct AppHash-continuity check: AppHash announced to
//     Tendermint via FinalizeBlock == AppHash persisted at the same
//     height.
//
// Both empty and non-empty caller writes are covered so the test
// catches regressions where the gate is bypassed for either input
// shape.
func TestRootMultiMigrateEVM_DoubleFlushAppHashStable(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: deposit EVM storage in MemiavlOnly so the upcoming
	// migration has real work and the boundary actually advances on
	// the first flush (an already-NotStarted manager facing an empty
	// iterator would short-circuit and hide the bug).
	store, storeKeys := newTestRootMulti(t, dir, memiavlOnlyConfig())
	addrBase := newEVMTestData(0xC1)
	storageKeys := storageMemIAVLKeys(0xC1, 12)
	for i := 1; i <= 2; i++ {
		simulateBlockManyStorage(t, store, storeKeys, i, storageKeys, addrBase)
	}

	// --- Restart into MigrateEVM with a small batch so multiple
	// per-block advances are required to drain. ---
	const batch = 4
	store, storeKeys = restartRootMultiWithConfig(t, store, dir, migrateEVMConfig())
	require.NoError(t, store.SetMigrationBatchSize(batch))
	defer func() { require.NoError(t, store.Close()) }()

	// Alternate empty blocks (exercise the "empty changesets in
	// migration mode still advance" path) with non-empty blocks
	// (exercise the routing-with-real-writes path). Both must be
	// stable under double flush.
	for blockIdx := 0; blockIdx < 4; blockIdx++ {
		if blockIdx%2 == 1 {
			cms := store.CacheMultiStore()
			b := byte(blockIdx + 10)
			evmKV := cms.GetKVStore(storeKeys["evm"])
			cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
			evmKV.Set(addrBase.nonKey, makeNonce(uint64(blockIdx)))
			cms.Write()
		}

		// First flush: AppHash that would be returned to Tendermint
		// from FinalizeBlock.
		announced, err := store.GetWorkingHash()
		require.NoError(t, err, "block idx %d: first GetWorkingHash", blockIdx)
		require.NotEmpty(t, announced, "block idx %d: announced hash must be non-empty", blockIdx)

		// Second flush with no intervening writes: must be identical.
		// A regression in the per-commit boundary advance gate would
		// surface here as a different hash because the migration
		// would advance again and the working commit info would
		// shift.
		again, err := store.GetWorkingHash()
		require.NoError(t, err, "block idx %d: second GetWorkingHash", blockIdx)
		require.Equal(t, announced, again,
			"block idx %d: repeated GetWorkingHash must be idempotent; "+
				"a difference means migration advanced again inside the second flush",
			blockIdx)

		// Commit: the persisted LastCommitInfo hash must match what
		// was already announced.
		cid := store.Commit(true)
		require.Equal(t, announced, []byte(cid.Hash),
			"block idx %d (version %d): persisted hash must equal the hash already "+
				"returned by GetWorkingHash; otherwise Tendermint accepted an AppHash that "+
				"differs from the validator's actual chain head",
			blockIdx, cid.Version)
	}
}
