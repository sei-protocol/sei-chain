package composite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// backendID identifies which backend a store's keys live on after a given
// mode reaches its steady state. Used by the end-of-test deep inspection.
type backendID int

const (
	backendMemiavl backendID = iota
	backendFlatKV
	// backendDualWriteEVM marks the keys.EVMStoreKey under TestOnlyDualWrite:
	// keys must be present in BOTH memiavl AND flatkv. Other dual-write
	// stores stay on memiavl only and use backendMemiavl.
	backendDualWriteEVM
)

// modeProfile captures everything the fuzz suite needs to drive and verify
// a single config.WriteMode end-to-end. The registry returned by
// allModeProfiles is the single source of truth for per-mode behavior;
// individual tests must not switch on the write mode directly.
type modeProfile struct {
	// name is the human-readable mode name used in t.Run sub-tests.
	name string

	// writeMode is the WriteMode under test.
	writeMode config.WriteMode

	// keysToMigratePerBlock seeds StateCommitConfig.KeysToMigratePerBlock.
	// Required to be > 0 by config.Validate; non-migrating modes still
	// supply a positive value to keep the config valid even though the
	// migration manager is not in the data path.
	keysToMigratePerBlock int

	// initialStores is passed to cs.Initialize. The fuzz workload only
	// touches stores in this list when generating new keys.
	initialStores []string

	// iterableStores is the set of stores whose data is reachable via a
	// route that supports Iterator() in this mode. The workload restricts
	// per-block Iterator calls to this set; deep inspection ignores it.
	iterableStores map[string]bool

	// proofSupportingStores is the set of stores whose data is reachable
	// via a route that supports GetProof() in this mode. Always a subset
	// of iterableStores because only memiavl supplies ICS23 proofs and
	// every memiavl-routed store also supports iteration.
	proofSupportingStores map[string]bool

	// hasMemiavl reports whether cs.memIAVL is non-nil in this mode.
	hasMemiavl bool

	// hasFlatKV reports whether cs.flatKV is non-nil in this mode.
	hasFlatKV bool

	// isActiveMigration reports whether the migration manager is in the
	// data path for this mode (MigrateEVM / MigrateAllButBank /
	// MigrateBank). The CRUD and reopen suites must drive enough blocks
	// for active-migration modes to complete migration before the
	// end-of-test deep inspection runs.
	isActiveMigration bool

	// finalPlacement maps each module name to the backend its keys live
	// on after this mode reaches its post-migration steady state. The
	// deep inspector consults this map directly. For modes where
	// hasMemiavl is false, every entry must be backendFlatKV; for modes
	// where hasFlatKV is false, every entry must be backendMemiavl;
	// for TestOnlyDualWrite, keys.EVMStoreKey is backendDualWriteEVM.
	finalPlacement map[string]backendID
}

// allModeProfiles returns the per-mode profile table consumed by the
// table-driven fuzz tests. Coverage: all 8 values of config.WriteMode.
//
// Initial stores are always keys.MemIAVLStoreKeys: cs.Initialize is a no-op
// when memiavl is nil (FlatKVOnly) and accepts the canonical set in every
// other mode. Migration metadata is mounted on flatkv where applicable, not
// on memiavl, so we do not include migration.MigrationStore here.
func allModeProfiles() []modeProfile {
	allMem := makeBackendMap(keys.MemIAVLStoreKeys, backendMemiavl)
	allFlat := makeBackendMap(keys.MemIAVLStoreKeys, backendFlatKV)
	evmFlatRestMem := makeBackendMap(keys.MemIAVLStoreKeys, backendMemiavl)
	evmFlatRestMem[keys.EVMStoreKey] = backendFlatKV
	bankMemRestFlat := makeBackendMap(keys.MemIAVLStoreKeys, backendFlatKV)
	bankMemRestFlat[keys.BankStoreKey] = backendMemiavl
	dualWrite := makeBackendMap(keys.MemIAVLStoreKeys, backendMemiavl)
	dualWrite[keys.EVMStoreKey] = backendDualWriteEVM

	allKeysSet := stringSliceToSet(keys.MemIAVLStoreKeys)
	allButEVM := stringSliceToSet(keys.MemIAVLStoreKeys)
	delete(allButEVM, keys.EVMStoreKey)
	onlyBank := map[string]bool{keys.BankStoreKey: true}
	empty := map[string]bool{}

	return []modeProfile{
		{
			name:                  "MemiavlOnly",
			writeMode:             config.MemiavlOnly,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			iterableStores:        allKeysSet,
			proofSupportingStores: allKeysSet,
			hasMemiavl:            true,
			hasFlatKV:             false,
			isActiveMigration:     false,
			finalPlacement:        allMem,
		},
		{
			name:                  "MigrateEVM",
			writeMode:             config.MigrateEVM,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			// During the in-flight migration the EVM route is owned by
			// the migration manager and does not expose Iterator; the
			// suite drives enough blocks to complete migration and only
			// then runs deep inspection. The per-block iterator sample
			// stays on non-EVM modules throughout, where it is always
			// safe.
			iterableStores:        allButEVM,
			proofSupportingStores: allButEVM,
			hasMemiavl:            true,
			hasFlatKV:             true,
			isActiveMigration:     true,
			finalPlacement:        evmFlatRestMem,
		},
		{
			name:                  "EVMMigrated",
			writeMode:             config.EVMMigrated,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			iterableStores:        allButEVM,
			proofSupportingStores: allButEVM,
			hasMemiavl:            true,
			hasFlatKV:             true,
			isActiveMigration:     false,
			finalPlacement:        evmFlatRestMem,
		},
		{
			name:                  "MigrateAllButBank",
			writeMode:             config.MigrateAllButBank,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			// In MigrateAllButBank the migration manager owns every
			// non-bank-non-evm module, EVM is on flatkv, and only bank
			// stays on memiavl. Restrict per-block Iterator sampling to
			// bank.
			iterableStores:        onlyBank,
			proofSupportingStores: onlyBank,
			hasMemiavl:            true,
			hasFlatKV:             true,
			isActiveMigration:     true,
			finalPlacement:        bankMemRestFlat,
		},
		{
			name:                  "AllMigratedButBank",
			writeMode:             config.AllMigratedButBank,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			iterableStores:        onlyBank,
			proofSupportingStores: onlyBank,
			hasMemiavl:            true,
			hasFlatKV:             true,
			isActiveMigration:     false,
			finalPlacement:        bankMemRestFlat,
		},
		{
			name:                  "MigrateBank",
			writeMode:             config.MigrateBank,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			// During the bank migration the bank route is owned by the
			// migration manager and every other module routes to flatkv.
			// Nothing is safely iterable.
			iterableStores:        empty,
			proofSupportingStores: empty,
			hasMemiavl:            true,
			hasFlatKV:             true,
			isActiveMigration:     true,
			finalPlacement:        allFlat,
		},
		{
			name:                  "FlatKVOnly",
			writeMode:             config.FlatKVOnly,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			iterableStores:        empty,
			proofSupportingStores: empty,
			hasMemiavl:            false,
			hasFlatKV:             true,
			isActiveMigration:     false,
			finalPlacement:        allFlat,
		},
		{
			name:                  "TestOnlyDualWrite",
			writeMode:             config.TestOnlyDualWrite,
			keysToMigratePerBlock: 1024,
			initialStores:         keys.MemIAVLStoreKeys,
			// Dual-write keeps every key in memiavl (the primary) and
			// additionally mirrors evm keys to flatkv. Reads route
			// through memiavl, so iteration is safe for every store.
			iterableStores:        allKeysSet,
			proofSupportingStores: allKeysSet,
			hasMemiavl:            true,
			hasFlatKV:             true,
			isActiveMigration:     false,
			finalPlacement:        dualWrite,
		},
	}
}

// activeMigrationProfiles returns the subset of profiles whose router
// performs background data migration. Used by the parallel-replica
// state-sync test, which only applies to active-migration modes.
func activeMigrationProfiles() []modeProfile {
	all := allModeProfiles()
	out := make([]modeProfile, 0, 3)
	for _, p := range all {
		if p.isActiveMigration {
			out = append(out, p)
		}
	}
	return out
}

// compositeOption mutates the StateCommitConfig that newCompositeForMode
// builds for the test's CompositeCommitStore. Tests that need behavior
// outside the defaults (e.g. dense snapshots for the exporter) pass
// option funcs to newCompositeForMode.
type compositeOption func(*config.StateCommitConfig)

// withFlatKVSnapshotPerBlock forces flatkv to take a pebble checkpoint
// snapshot on every committed version. Required by tests that open the
// flatkv Exporter at a version below which the WAL is non-contiguous —
// most importantly TestCompositeFuzzStateSyncDuringMigration, where
// flatkv is seeded mid-test by a MemiavlOnly → MigrateEVM transition
// and its WAL therefore has no entries below the seed version. The
// readonly catchup loads committedVersion from the most recent
// snapshot's metadata DB, so a snapshot at or below the export version
// is required for the catchup to start at the seeded version rather
// than at 0.
//
// This option is expensive: every commit produces a new on-disk
// checkpoint (≈200 ms each in CI), so it is opt-in and only applied
// where the test design needs it.
func withFlatKVSnapshotPerBlock() compositeOption {
	return func(cfg *config.StateCommitConfig) {
		cfg.FlatKVConfig.SnapshotInterval = 1
	}
}

// withSnapshotKeepRecent overrides SnapshotKeepRecent on both backends.
// The default (2) is too low for tests that need to roll back across
// many blocks: both memiavl.Rollback and flatkv.Rollback locate the
// snapshot at-or-below the rollback target, and a too-aggressively
// pruned snapshot makes the operation fail with "no snapshot found for
// target version N".
//
// Memiavl-only modes still set keep-recent on flatkv even though flatkv
// is not allocated, because the option is mode-agnostic; the unused
// config field is a no-op.
func withSnapshotKeepRecent(keep uint32) compositeOption {
	return func(cfg *config.StateCommitConfig) {
		cfg.FlatKVConfig.SnapshotKeepRecent = keep
		cfg.MemIAVLConfig.SnapshotKeepRecent = keep
	}
}

// newCompositeForMode opens a CompositeCommitStore at dir for the given
// profile, with deterministic settings so reopen and exporter behavior is
// reproducible. Returns the loaded composite and registers no cleanup —
// callers manage Close themselves so the helper plays nicely with tests
// that reopen the same dir multiple times.
//
// Default tuning:
//
//   - memiavl AsyncCommitBuffer=0 / SnapshotInterval=1: GetLatestVersion
//     and the memiavl exporter observe on-disk state, which races with
//     the async commit buffer; a per-block snapshot also makes
//     historical Exporter calls trivial. The cost is negligible
//     compared to flatkv snapshots.
//   - flatkv SnapshotInterval is left at its default (10000 = no
//     snapshots in any reasonably-sized test run). Tests whose WAL is
//     contiguous from v=1 do not need flatkv snapshots and avoid the
//     ≈200 ms-per-commit checkpoint cost by relying on WAL replay
//     during readonly catchup.
//
// Per-test overrides come in as variadic compositeOption funcs.
func newCompositeForMode(
	t *testing.T,
	ctx context.Context,
	dir string,
	profile modeProfile,
	opts ...compositeOption,
) *CompositeCommitStore {
	t.Helper()

	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = profile.writeMode
	cfg.KeysToMigratePerBlock = profile.keysToMigratePerBlock

	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0

	for _, opt := range opts {
		opt(&cfg)
	}

	cs, err := NewCompositeCommitStore(ctx, dir, cfg)
	require.NoError(t, err, "NewCompositeCommitStore for %s", profile.name)
	require.NoError(t, cs.Initialize(profile.initialStores), "Initialize for %s", profile.name)

	_, err = cs.LoadVersion(0, false)
	require.NoError(t, err, "LoadVersion for %s", profile.name)

	// Mode-shape invariants. Cheap to check and surface mis-configured
	// modes loudly at construction time.
	if profile.hasMemiavl {
		require.NotNil(t, cs.memIAVL, "%s must allocate memiavl", profile.name)
	} else {
		require.Nil(t, cs.memIAVL, "%s must not allocate memiavl", profile.name)
	}
	if profile.hasFlatKV {
		require.NotNil(t, cs.flatKV, "%s must allocate flatkv", profile.name)
	} else {
		require.Nil(t, cs.flatKV, "%s must not allocate flatkv", profile.name)
	}

	return cs
}

// makeBackendMap returns a map from every module name in stores to b.
func makeBackendMap(stores []string, b backendID) map[string]backendID {
	out := make(map[string]backendID, len(stores))
	for _, s := range stores {
		out[s] = b
	}
	return out
}

// stringSliceToSet returns ss as a set.
func stringSliceToSet(ss []string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}
