package composite

// FlatKV archive-validation harness (Arm A) — the three gap mechanisms (HLD §4c), each exercising a
// surface real history can't reach: schedule-divergence, torn-write between backend commits, and
// state-sync resume. Test-only; tracks PLT-680.

import (
	"bytes"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/stretchr/testify/require"
)

const gapCorpus = "testdata/flatkv-corpus/window_straddling-1"

// replayCapturingRoots replays a corpus at a given schedule, returning the live store, the oracle,
// and the flatkv committed root after every block.
func replayCapturingRoots(t *testing.T, c *harnessCorpus, batch int) (*CompositeCommitStore, *storeOracle, [][]byte) {
	t.Helper()
	oracle := newStoreOracle()
	dir := seedCorpusBoundary(t, c, oracle)
	cs := reopenInMigrateEVM(t, dir, batch)
	roots := make([][]byte, 0, len(c.Blocks))
	for _, blk := range c.Blocks {
		applyCorpusBlock(t, cs, oracle, blk)
		roots = append(roots, append([]byte(nil), cs.flatKV.CommittedRootHash()...))
	}
	return cs, oracle, roots
}

// TestHarness_Gap_ScheduleDivergence drives the same corpus through two migration schedules (K=2 vs
// K=3) and pins down the central correctness property of an AppHash-breaking migration:
//
//	TRUTH (logical state) is schedule-INVARIANT, but the committed lattice ROOT is schedule-DEPENDENT.
//
// The root depends on the schedule because the flatkv value encoding embeds the block height a key
// is written/migrated at (vtype SetBlockHeight, store_apply.go) and the lattice element is hashed
// over that serialized value; a boundary key migrated under a different K lands at a different
// height and so contributes a different element — even though its logical slot value is identical.
//
// Two operational consequences this test guards:
//  1. Validation must compare LOGICAL CONTENT, never the root — the root cannot be an equality
//     oracle across schedules (this is why Arm A's truth check is the logical fold, not an AppHash).
//  2. The migration schedule (KeysToMigratePerBlock) must be identical across all nodes, or they
//     compute different roots mid-migration and fork. The schedule is a consensus-relevant input.
func TestHarness_Gap_ScheduleDivergence(t *testing.T) {
	c, err := loadHarnessCorpus(gapCorpus)
	require.NoError(t, err)

	csA, oracleA, rootsA := replayCapturingRoots(t, c, 2)
	defer func() { _ = csA.Close() }()
	csB, oracleB, rootsB := replayCapturingRoots(t, c, 3)
	defer func() { _ = csB.Close() }()

	// Both schedules must drain within the corpus, or the comparison is meaningless.
	require.True(t, migrationComplete(t, csA), "K=2 schedule must complete within the corpus")
	require.True(t, migrationComplete(t, csB), "K=3 schedule must complete within the corpus")

	// TRUTH is schedule-invariant: identical corpus -> identical final logical state, both self-consistent.
	require.Equal(t, oracleA.stores, oracleB.stores, "final logical state must not depend on the schedule")
	require.NoError(t, flatkv.VerifyLtHash(csA.flatKV))
	require.NoError(t, flatkv.VerifyLtHash(csB.flatKV))

	// ROOT is schedule-dependent: different schedules -> different committed root at completion,
	// despite identical logical state. This is the AppHash-breaking property, demonstrated.
	require.False(t, bytes.Equal(rootsA[len(rootsA)-1], rootsB[len(rootsB)-1]),
		"committed root must differ across schedules (embedded block height) — it is not a cross-schedule oracle")
}
