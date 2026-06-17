package composite

// FlatKV archive-validation harness (Arm A) — the three gap mechanisms, each exercising a surface
// real history can't reach: schedule-divergence, torn-write between backend commits, and state-sync
// resume. Test-only.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/stretchr/testify/require"
)

// errTornWriteInjected is the sentinel the torn-write hook panics with, so the recover can tell an
// injected tear from a genuine crash.
var errTornWriteInjected = errors.New("torn-write hook fired")

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

// tearAtNextCommit applies a block then drives Commit into the torn-write seam: memIAVL.Commit
// runs and durably advances one version, the hook panics before flatKV.Commit, and the recover
// returns control with the two backends one version apart on disk. The block is deliberately NOT
// mirrored into the oracle — it is the write that gets rolled back.
func tearAtNextCommit(t *testing.T, cs *CompositeCommitStore, blk harnessBlock) {
	t.Helper()
	named, err := blk.toNamedChangeSet()
	require.NoError(t, err)
	require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{named}))

	hook := func() { panic(errTornWriteInjected) }
	innerCommitHookForTest.Store(&hook)
	// Disarm at function scope, not via t.Cleanup: the caller resumes committing after this returns,
	// and a still-armed hook would panic those commits too.
	defer innerCommitHookForTest.Store(nil)

	defer func() {
		require.Equal(t, errTornWriteInjected, recover(), "expected the torn-write hook to fire inside Commit")
	}()
	_, _ = cs.Commit()
	t.Fatal("Commit returned without firing the torn-write hook")
}

// TestHarness_Gap_TornWrite exercises the crash-between-the-two-backend-commits seam that the public
// API cannot otherwise reach. The migration commits memIAVL then flatKV sequentially; a crash in
// between leaves memIAVL one version ahead. Reopen must run reconcileVersions to roll the ahead
// backend back to the consistent version, and the migration must then resume to a correct result.
// The existing crash/resume coverage only crashes at clean version boundaries, so this is the only
// test that drives the reconcile path.
func TestHarness_Gap_TornWrite(t *testing.T) {
	c, err := loadHarnessCorpus(gapCorpus)
	require.NoError(t, err)

	const batch = 2 // spreads the 8-key boundary migration across commits so the tear lands mid-migration
	oracle := newStoreOracle()
	dir := seedCorpusBoundary(t, c, oracle)
	cs := reopenInMigrateEVM(t, dir, batch)

	// One clean block, then capture the consistent version we expect the reconcile to restore.
	applyCorpusBlock(t, cs, oracle, c.Blocks[0])
	consistentVersion := cs.Version()
	require.False(t, migrationComplete(t, cs), "tear must land mid-migration; lower batch or extend corpus")

	// Tear the next block. Proving the tear is real (not a vacuous pass): memIAVL must have
	// durably advanced one version while flatKV stayed put, so reconcile has genuine work to do.
	tearAtNextCommit(t, cs, c.Blocks[1])
	require.Equal(t, consistentVersion+1, cs.memIAVL.Version(), "memIAVL must advance one version into the tear")
	require.Equal(t, consistentVersion, cs.flatKV.Version(), "flatKV must remain at the pre-tear version")
	require.NoError(t, cs.Close())

	// Reopen: LoadVersion runs reconcileVersions, which must roll the ahead backend back so both
	// backends agree at the last consistent version, and the torn block's writes are gone.
	cs = reopenInMigrateEVM(t, dir, batch)
	require.Equal(t, consistentVersion, cs.Version(), "reconcile must restore the last consistent version")
	require.Equal(t, cs.memIAVL.Version(), cs.flatKV.Version(), "backends must agree after reconcile")
	verifyOracle(t, cs, oracle)
	require.NoError(t, flatkv.VerifyLtHash(cs.flatKV))

	// Resume: replay the torn block and the remainder; the migration must complete to a correct result.
	for _, blk := range c.Blocks[1:] {
		applyCorpusBlock(t, cs, oracle, blk)
	}
	assertHarnessVerdict(t, cs, oracle, c)
	require.True(t, migrationComplete(t, cs), "migration must complete after resume")
	_ = cs.Close()
}

// TestHarness_Gap_StateSyncResume covers the state-sync seam: a node bootstrapped from a snapshot
// (Exporter -> Importer) must land with both backends at the same version, the logical state
// intact, and the next block committing cleanly. The existing export/import test stops at reload;
// this adds the two assertions that are the actual gap — post-import backend-version alignment and
// V+1 continuity — driven by a fully-migrated corpus.
func TestHarness_Gap_StateSyncResume(t *testing.T) {
	c, err := loadHarnessCorpus(gapCorpus)
	require.NoError(t, err)
	stores := []string{keys.BankStoreKey, keys.EVMStoreKey}

	// Replay the corpus to migration completion, then close.
	oracle := newStoreOracle()
	dir := seedCorpusBoundary(t, c, oracle)
	cs := reopenInMigrateEVM(t, dir, c.Manifest.Schedule.KeysToMigratePerBlock)
	for _, blk := range c.Blocks {
		applyCorpusBlock(t, cs, oracle, blk)
	}
	require.True(t, migrationComplete(t, cs), "corpus must fully migrate before the state-sync export")
	require.NoError(t, cs.Close())

	// Reopen in the post-migration steady state (snapshotting on) and mint one snapshot by
	// re-applying the last block — same keys/values, so the logical state is unchanged.
	cfg := evmMigratedConfig()
	src, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, src.Initialize(stores))
	_, err = src.LoadVersion(0, false)
	require.NoError(t, err)
	applyCorpusBlock(t, src, oracle, c.Blocks[len(c.Blocks)-1])
	syncVersion := src.Version()

	exporter, err := src.Exporter(syncVersion)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, src.Close())

	// Bootstrap a fresh node from the snapshot.
	dstDir := t.TempDir()
	dst, err := NewCompositeCommitStore(t.Context(), dstDir, cfg)
	require.NoError(t, err)
	require.NoError(t, dst.Initialize(stores))
	_, err = dst.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, dst.Close())

	importer, err := dst.Importer(syncVersion)
	require.NoError(t, err)
	replayImport(t, importer, items)
	require.NoError(t, importer.Close())

	// Reload at the synced version — this runs reconcileVersions.
	_, err = dst.LoadVersion(syncVersion, false)
	require.NoError(t, err)
	defer func() { _ = dst.Close() }()

	// The gap: both backends must be aligned at the synced version, with the logical state intact.
	require.Equal(t, syncVersion, dst.Version(), "synced node must load at the snapshot version")
	require.Equal(t, dst.memIAVL.Version(), dst.flatKV.Version(),
		"first-post-snapshot alignment: backends must agree after a state-sync import")
	verifyOracle(t, dst, oracle)
	require.NoError(t, flatkv.VerifyLtHash(dst.flatKV))

	// Continuity: the first commit after the snapshot must advance one version and stay consistent.
	// The corpus is fully replayed, so we re-commit the tail block (same keys/values, logical no-op)
	// purely to drive one more commit through the synced node.
	applyCorpusBlock(t, dst, oracle, c.Blocks[len(c.Blocks)-1])
	require.Equal(t, syncVersion+1, dst.Version(), "first post-snapshot commit must advance one version")
	verifyOracle(t, dst, oracle)
	require.NoError(t, flatkv.VerifyLtHash(dst.flatKV))
}
