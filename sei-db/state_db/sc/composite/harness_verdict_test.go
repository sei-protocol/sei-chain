package composite

// FlatKV archive-validation harness (Arm A) — the verdict.
// Renders the harness's axes over a replayed corpus, fail-closed (no "warn"):
//   TRUTH       — composite reads match the fold of the applied changesets (routing), and that
//                 fold matches corpus-gen's independent v0 expected_state. This is the load-bearing
//                 check; it is NOT an AppHash/lattice-root compare (the migration is AppHash-breaking).
//   CONSISTENCY — on-disk flatkv content matches its own committed lattice root (VerifyLtHash).
//   STRUCTURAL  — once the migration drains, the EVM keyspace lives entirely in flatkv with no
//                 memiavl residue.
// Determinism is asserted across stores in the schedule-divergence gap test, not per-corpus here.
// Placement/key-count exactness is already covered by the steady-state migration suite, so the
// verdict does not re-litigate it. Test-only.

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/stretchr/testify/require"
)

// migrationComplete reports whether the flatkv migration-version key has reached Version1_MigrateEVM.
func migrationComplete(t *testing.T, cs *CompositeCommitStore) bool {
	t.Helper()
	done, err := migration.IsAtVersion(flatKVReaderFor(cs), uint64(migration.Version1_MigrateEVM))
	require.NoError(t, err)
	return done
}

// requireEVMFullyInFlatKV asserts the completion-residue invariant: every live EVM key resolves in
// flatkv and none remains in memiavl. Valid only once the migration has drained.
func requireEVMFullyInFlatKV(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle) {
	t.Helper()
	for k := range oracle.stores[keys.EVMStoreKey] {
		key := []byte(k)
		_, memFound := memiavlGetForTest(cs, keys.EVMStoreKey, key)
		_, flatFound := flatKVGetForTest(cs, keys.EVMStoreKey, key)
		require.Falsef(t, memFound, "post-completion: EVM key %x must not remain in memiavl", key)
		require.Truef(t, flatFound, "post-completion: EVM key %x must resolve in flatkv", key)
	}
}

// assertHarnessVerdict renders the full verdict over a replayed corpus. Any single mismatch fails.
func assertHarnessVerdict(t *testing.T, cs *CompositeCommitStore, oracle *storeOracle, c *harnessCorpus) {
	t.Helper()
	// TRUTH — routing and the independent v0 fold.
	verifyOracle(t, cs, oracle)
	requireOracleMatchesExpected(t, oracle, c)
	// CONSISTENCY — on-disk content vs its own committed lattice root.
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))
	// The merged composite iterator returns exactly the oracle's union, in order. When the boundary
	// still straddles both backends this is the cross-backend stitch; TestHarness_MovingBoundary
	// drives that mid-migration explicitly, since the verdict often runs at the drained end state.
	verifyIteration(t, cs, oracle, []string{keys.BankStoreKey, keys.EVMStoreKey})
	// STRUCTURAL — completion residue, once the schedule has drained.
	if migrationComplete(t, cs) {
		requireEVMFullyInFlatKV(t, cs, oracle)
	}
}
