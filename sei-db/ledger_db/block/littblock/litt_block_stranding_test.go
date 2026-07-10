package littblock

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// strandingConfig builds a config whose only segment-rollover trigger is a small
// MaxSegmentKeyCount, so a test can place a segment boundary at a precise key.
// Retention is tiny (the prune watermark is the sole reclamation gate) and GC is
// effectively background-disabled so ForceGC is the only thing that reclaims.
func strandingConfig(t *testing.T, dir string, maxSegmentKeyCount uint32) *LittBlockConfig {
	cfg, err := DefaultConfig(dir)
	require.NoError(t, err)
	cfg.Retention = time.Nanosecond
	cfg.Litt.TargetSegmentFileSize = math.MaxUint32
	cfg.Litt.MaxSegmentKeyCount = maxSegmentKeyCount
	cfg.Litt.GCPeriod = time.Hour
	cfg.Litt.Fsync = false
	return cfg
}

// writeSyntheticBatches writes numBatches contiguous batches of perQC blocks each
// (global numbers 0.., QC ranges [0,perQC), [perQC,2*perQC), ...). The QCs carry
// perQC opaque headers so the store's range accounting (Next = First + len) lines
// up; littblock does not verify signatures, so no committee is needed.
func writeSyntheticBatches(t *testing.T, db types.BlockDB, rng utils.Rng, numBatches int, perQC int) {
	for i := 0; i < numBatches; i++ {
		first := types.GlobalBlockNumber(i * perQC) //nolint:gosec // small test indices
		next := first + types.GlobalBlockNumber(perQC)
		qc := types.GenFullCommitQCN(rng, perQC)
		require.NoError(t, db.WriteQC(first, next, qc))
		for j := 0; j < perQC; j++ {
			require.NoError(t, db.WriteBlock(first+types.GlobalBlockNumber(j), types.GenBlock(rng))) //nolint:gosec
		}
	}
}

// physicallyPresent reports whether a key exists in the raw table, bypassing the
// read-watermark gate. Used to distinguish a record that has been physically
// reclaimed from one that is present on disk but refused by the watermark.
func physicallyPresent(t *testing.T, impl *blockDB, key []byte) bool {
	_, exists, err := impl.table.Get(key)
	require.NoError(t, err)
	return exists
}

// TestLittblockStrandedBlockNotServedAfterRestart is the cross-segment stranding
// regression. GC reclaims a contiguous prefix of segments in write order, and a
// QC is always written before the blocks it covers, so a QC's covered range can
// straddle a segment boundary and its segment can be reclaimed while a later
// segment still holds some covered blocks — leaving those blocks on disk with no
// covering QC. Because the prune watermark is in-memory only, a restart forgets
// it and a naive store would re-serve the stranded blocks.
//
// With MaxSegmentKeyCount = 8 the QC Put (5 keys) plus the first two block Puts
// (2 keys each) fill segment 0 = {QC[0,5), b0, b1}; the remaining covered blocks
// spill into segment 1 = {b2, b3, b4, QC[5,10)}. Pruning to 5 makes segment 0
// collectable (every key < 5) while segment 1 is pinned by QC[5,10) (key 5), so
// QC[0,5) is reclaimed but blocks 2..4 survive, stranded.
func TestLittblockStrandedBlockNotServedAfterRestart(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(1)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19; QCs [0,5),[5,10),[10,15),[15,20)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen so the segments seal, then prune past the first QC and collect.
	db2, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	require.NoError(t, db2.PruneBefore(5))
	require.NoError(t, ForceGC(db2))
	require.NoError(t, db2.Flush())
	require.NoError(t, db2.Close())

	// Reopen with the in-memory watermark forgotten: this is where a store that
	// did not re-derive the watermark would re-serve the stranded blocks 2..4.
	db3, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	defer func() { _ = db3.Close() }()
	impl := db3.(*blockDB)

	// The stranding really materialized on disk: block 2 is physically present
	// but its covering QC's key is gone (reclaimed with segment 0). Blocks 0 and
	// 1 were in the reclaimed segment and are physically gone.
	require.True(t, physicallyPresent(t, impl, blockKey(2)), "block 2 must be physically stranded on disk")
	require.False(t, physicallyPresent(t, impl, qcKey(2)), "covering QC key for block 2 must be reclaimed")
	require.False(t, physicallyPresent(t, impl, blockKey(0)), "block 0 must be physically reclaimed")
	require.False(t, physicallyPresent(t, impl, blockKey(1)), "block 1 must be physically reclaimed")

	// The watermark is re-derived as the lowest surviving QC's First.
	require.Equal(t, uint64(5), impl.watermark.Load(), "recovered watermark must be the lowest surviving QC's First")

	// The gate refuses the stranded blocks and their (absent) QCs.
	for n := types.GlobalBlockNumber(0); n < 5; n++ {
		blk, err := db3.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.False(t, blk.IsPresent(), "stranded/pruned block %d must not be served", n)
		qc, err := db3.ReadQCByBlockNumber(n)
		require.NoError(t, err)
		require.False(t, qc.IsPresent(), "QC for pruned block %d must not be served", n)
	}

	// Every served block has a readable covering QC (the invariant).
	for n := types.GlobalBlockNumber(5); n < 20; n++ {
		blk, err := db3.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "block %d must be served", n)
		qc, err := db3.ReadQCByBlockNumber(n)
		require.NoError(t, err)
		require.True(t, qc.IsPresent(), "covering QC for served block %d must be readable", n)
	}

	// The block iterator never yields a stranded block, and each yielded block
	// has a covering QC.
	it, err := db3.Blocks(false)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		n := it.Number()
		require.GreaterOrEqual(t, uint64(n), uint64(5), "iterator must not yield stranded block %d", n)
		qc, err := db3.ReadQCByBlockNumber(n)
		require.NoError(t, err)
		require.True(t, qc.IsPresent(), "iterated block %d must have a covering QC", n)
	}
}

// TestLittblockReclaimsAcrossRestart verifies the durable reclamation path with a
// white-box check on the raw table: data written, then pruned after a restart
// (which seals the segments it landed in), is physically collected by GC. A
// raw-table check is required because the read watermark now refuses
// below-watermark reads regardless of physical reclamation, so public reads alone
// can no longer distinguish "reclaimed" from "gated".
//
// PruneBefore(20) requests pruning past every block, but the never-empty
// invariant clamps it to the newest block's cohort — the QC[15,20) range, whose
// first is 15. Blocks 15..19 and QC[15,20) stay readable, pinning their segments
// (seg5={QC[15,20),b15,b16}, seg6={b17,b18,b19}) against GC. Every fully-below
// segment — blocks 0..14 and QCs [0,5),[5,10),[10,15) — is reclaimed.
func TestLittblockReclaimsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(2)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen: the segments written above are now sealed and collectable.
	db2, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()
	impl := db2.(*blockDB)

	require.NoError(t, db2.PruneBefore(20)) // past every block; capped to 19 (never-empty)
	require.NoError(t, ForceGC(db2))

	// Blocks 0..14 and their QC keys are physically reclaimed.
	for n := types.GlobalBlockNumber(0); n < 15; n++ {
		require.False(t, physicallyPresent(t, impl, blockKey(n)), "block %d must be reclaimed", n)
		require.False(t, physicallyPresent(t, impl, qcKey(n)), "QC key %d must be reclaimed", n)
	}

	// The newest block and its covering QC survive the capped prune: the cap
	// pins block 19's segment and the straddling QC[15,20) against GC.
	for n := types.GlobalBlockNumber(15); n < 20; n++ {
		require.True(t, physicallyPresent(t, impl, blockKey(n)), "block %d must survive the capped prune", n)
	}
	require.True(t, physicallyPresent(t, impl, qcKey(15)), "covering QC[15,20) must survive the capped prune")

	// The whole newest cohort is served through the public API: the clamp lands
	// on the cohort's first (15), so blocks 15..19 and their covering QC are
	// readable, while block 14 (just below the cohort) is refused.
	for n := types.GlobalBlockNumber(15); n < 20; n++ {
		blk, err := db2.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "cohort block %d must remain readable after a prune-to-empty request", n)
		qc, err := db2.ReadQCByBlockNumber(n)
		require.NoError(t, err)
		require.True(t, qc.IsPresent(), "QC covering cohort block %d must remain readable", n)
	}
	below, err := db2.ReadBlockByNumber(14)
	require.NoError(t, err)
	require.False(t, below.IsPresent(), "block 14 below the newest cohort must not be served")
}

// TestLittblockPartiallyPrunedQCRangeRetained pins the partial-prune rule for a
// single QC's covered range: when the watermark falls strictly inside a QC's
// range, pruning some of the range's early blocks (but not all) retains the
// ENTIRE range on disk. The QC straddles the watermark so it can never be
// reclaimed, and because a QC is always written before the blocks it covers, its
// segment sits at or before every block in its range in write order. GC reclaims
// only a contiguous write-order prefix of segments, so pinning the QC's segment
// pins every later segment too — every block in the range survives, including the
// sub-watermark ones. The read gate, not physical reclamation, is what hides
// those sub-watermark blocks from callers.
//
// The test is written so it would FAIL if the straddle invariant were broken:
//   - It first asserts the fully-below segment (seg0) IS reclaimed. LittDB never
//     GCs the live mutable file, so this is what proves GC actually ran with
//     teeth on this data — without it the retention assertions below would hold
//     vacuously (nothing ever collected). The write→flush→close→reopen dance
//     seals the mutable file the data landed in so GC can reach it.
//   - If the QC were wrongly treated as reclaimable, GC would collect seg1 (it
//     sits right after seg0 in the reclaimed prefix), deleting QC[5,10). Blocks
//     7..9 stay served (their own segments are pinned by their at/above-watermark
//     keys), so ReadQCByBlockNumber(7..9) would then return None — a served block
//     with no covering QC — and the read-semantics assertions would fail.
//
// Layout (MaxSegmentKeyCount = 8, as in TestLittblockStrandedBlockNotServedAfterRestart):
// seg0 = {QC[0,5), b0, b1}, seg1 = {b2, b3, b4, QC[5,10)}, seg2 = {b5, b6, b7, b8},
// seg3 = {b9, ...}. Pruning to 7 makes seg0 fully collectable, but leaves
// QC[5,10)'s secondary keys 7,8,9 at or above the watermark, so seg1 is pinned
// and every segment after it with it — blocks 5..9 all survive.
func TestLittblockPartiallyPrunedQCRangeRetained(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(3)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	writeSyntheticBatches(t, db, rng, 4, 5) // blocks 0..19; QC[5,10) covers blocks 5..9
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen to seal the mutable file the data landed in, then prune to 7 —
	// strictly inside QC[5,10).
	db2, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()
	impl := db2.(*blockDB)

	require.NoError(t, db2.PruneBefore(7))
	require.NoError(t, ForceGC(db2))

	// GC actually had teeth: the fully-below segment (seg0 = {QC[0,5), b0, b1}) is
	// reclaimed. If this fails, the data never made it out of the mutable file and
	// the retention assertions below would be vacuous.
	require.False(t, physicallyPresent(t, impl, blockKey(0)), "block 0 (fully below watermark) must be reclaimed")
	require.False(t, physicallyPresent(t, impl, blockKey(1)), "block 1 (fully below watermark) must be reclaimed")
	require.False(t, physicallyPresent(t, impl, qcKey(0)), "QC[0,5) (entirely below watermark) must be reclaimed")

	// The straddling QC[5,10) is not reclaimed (primary key plus a secondary that
	// sits above the watermark).
	require.True(t, physicallyPresent(t, impl, qcKey(5)), "straddling QC[5,10) primary key must survive")
	require.True(t, physicallyPresent(t, impl, qcKey(9)), "straddling QC[5,10) secondary key must survive")

	// Its entire covered range survives on disk — including blocks 5 and 6, which
	// are below the watermark. Pinning the QC's segment pins the whole range.
	for n := types.GlobalBlockNumber(5); n < 10; n++ {
		require.True(t, physicallyPresent(t, impl, blockKey(n)),
			"block %d in the partially pruned QC range must be retained on disk", n)
	}

	// Read semantics over the straddled range: the sub-watermark blocks are
	// refused (even though present on disk), the at-or-above blocks are served,
	// and the covering QC is readable for every served block.
	for n := types.GlobalBlockNumber(5); n < 7; n++ {
		blk, err := db2.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.False(t, blk.IsPresent(), "sub-watermark block %d must not be served despite surviving on disk", n)
	}
	for n := types.GlobalBlockNumber(7); n < 10; n++ {
		blk, err := db2.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "block %d at/above watermark must be served", n)
		qc, err := db2.ReadQCByBlockNumber(n)
		require.NoError(t, err)
		require.True(t, qc.IsPresent(), "covering QC for served block %d must be readable", n)
	}
}

// TestLittblockRefusesToOpenWithStrandedBlocks verifies the corruption guard in
// recoverReadWatermark. The never-empty prune invariant guarantees at least one
// (block, QC) pair is always retained, so a store holding a block with no
// surviving QC is corrupt (e.g. a QC WAL file removed out of band). Rather than
// serve blocks it can no longer trust, the store refuses to open.
//
// The state is unreachable through the public API — WriteBlock rejects an
// uncovered block, and pruning never reclaims the newest cohort's QC — so the
// test injects it directly: a block primary key written straight to the raw
// table with no covering QC, exactly the on-disk shape corruption leaves behind.
func TestLittblockRefusesToOpenWithStrandedBlocks(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(3)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	impl := db.(*blockDB)

	// Bypass WriteBlock's QC guard and write a lone block to the raw table.
	require.NoError(t, impl.table.Put(blockKey(5), encodeBlock(5, types.GenBlock(rng))))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen: recovery finds the block but no QC, so it refuses to open.
	_, err = NewBlockDB(strandingConfig(t, dir, 8))
	require.Error(t, err)
	require.ErrorContains(t, err, "no surviving QC")
}

// TestLittblockEmptyStorePruneDoesNotReclaimLaterWrites is the regression for the
// empty-store prune bug: a PruneBefore on a store with no blocks must not advance
// the watermark, or blocks later written below the requested point would be
// refused by the read gate and physically reclaimed by GC. The watermark must
// never outrun the data it protects.
func TestLittblockEmptyStorePruneDoesNotReclaimLaterWrites(t *testing.T) {
	dir := t.TempDir()
	rng := utils.TestRngFromSeed(4)

	db, err := NewBlockDB(strandingConfig(t, dir, 8))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	impl := db.(*blockDB)

	// Prune well past where we are about to write, while the store is still empty.
	require.NoError(t, db.PruneBefore(1000))
	require.Equal(t, uint64(0), impl.watermark.Load(), "empty-store prune must not advance the watermark")

	// Write blocks 0..9 (below the pruned point) and force a GC pass.
	writeSyntheticBatches(t, db, rng, 2, 5) // blocks 0..9; QCs [0,5),[5,10)
	require.NoError(t, ForceGC(db))
	require.NoError(t, db.Flush())

	// Every written block survives GC on disk and is served by the read gate.
	for n := types.GlobalBlockNumber(0); n < 10; n++ {
		require.True(t, physicallyPresent(t, impl, blockKey(n)), "block %d must survive GC", n)
		blk, err := db.ReadBlockByNumber(n)
		require.NoError(t, err)
		require.True(t, blk.IsPresent(), "block %d must be served", n)
	}
}
