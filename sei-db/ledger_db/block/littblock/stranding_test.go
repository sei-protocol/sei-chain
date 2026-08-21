package littblock_test

import (
	"math"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/blocktest"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// strandingConfig builds a config whose only segment-rollover trigger is a small
// MaxSegmentKeyCount, so a test can place a segment boundary at a precise key.
// Retention is tiny (the prune watermark is the sole reclamation gate) and GC is
// effectively background-disabled so ForceGC is the only thing that reclaims.
func strandingConfig(t *testing.T, dir string, maxSegmentKeyCount uint32) *littblock.BlockDBConfig {
	t.Helper()
	cfg, err := littblock.DefaultConfig(dir)
	require.NoError(t, err)
	cfg.RetentionTime = time.Nanosecond
	cfg.Litt.TargetSegmentFileSize = math.MaxUint32
	cfg.Litt.MaxSegmentKeyCount = maxSegmentKeyCount
	cfg.Litt.GCPeriod = time.Hour
	cfg.Litt.Fsync = false
	return cfg
}

// physicallyPresent reports whether a record is still on disk. Reads are not
// gated on the watermark, so this distinguishes a record GC has reclaimed from
// one that is merely below the floor.
func physicallyPresent(t *testing.T, db blocktypes.BlockDB, kind blocktypes.RecordKind, n uint64) bool {
	t.Helper()
	_, exists, err := db.GetRecord(kind, n)
	require.NoError(t, err)
	return exists
}

// TestRecoveredWatermarkExcludesStrandedBlocks is the cross-segment stranding
// regression. GC reclaims a contiguous prefix of segments in write order, and a
// QC is always written before the blocks it covers, so a QC's covered range can
// straddle a segment boundary and its segment can be reclaimed while a later
// segment still holds some covered blocks — leaving those blocks on disk with no
// covering QC. Because the watermark is in-memory only, a restart forgets it, and
// a store that did not re-derive it would report a floor beneath the stranded
// blocks and let the layer above serve them.
//
// With MaxSegmentKeyCount = 8 the QC put (5 keys) plus the first two block puts
// (2 keys each, number and hash) fill segment 0 = {QC[0,5), b0, b1}; the
// remaining covered blocks spill into segment 1 = {b2, b3, b4, QC[5,10)}. Pruning
// to 5 makes segment 0 collectable (every key < 5) while segment 1 is pinned by
// QC[5,10) (key 5), so QC[0,5) is reclaimed but blocks 2..4 survive, stranded.
func TestRecoveredWatermarkExcludesStrandedBlocks(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, strandingConfig(t, dir, 8))
	blocktest.WriteCohorts(t, db, 4, 5) // blocks 0..19; QCs [0,5),[5,10),[10,15),[15,20)
	blocktest.WriteAppData(t, db, 4, 5)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen so the segments seal, then raise the floor past the first QC and collect.
	db2 := openLitt(t, strandingConfig(t, dir, 8))
	db2.SetPruneWatermark(5)
	require.NoError(t, littblock.ForceGC(db2))
	require.NoError(t, db2.Flush())
	require.NoError(t, db2.Close())

	// Reopen with the in-memory watermark forgotten. This is where a store that
	// did not re-derive it would report 0.
	db3 := openLitt(t, strandingConfig(t, dir, 8))
	defer func() { _ = db3.Close() }()

	// The stranding really materialized on disk: block 2 is physically present but
	// its covering QC's key is gone, reclaimed with segment 0. Blocks 0 and 1 were
	// in that segment and are gone with it.
	require.True(t, physicallyPresent(t, db3, blocktypes.KindBlock, 2), "block 2 must be physically stranded on disk")
	require.False(t, physicallyPresent(t, db3, blocktypes.KindQC, 2), "covering QC key for block 2 must be reclaimed")
	require.False(t, physicallyPresent(t, db3, blocktypes.KindBlock, 0), "block 0 must be physically reclaimed")
	require.False(t, physicallyPresent(t, db3, blocktypes.KindBlock, 1), "block 1 must be physically reclaimed")

	// The watermark is re-derived as the lowest surviving QC's first number, which
	// puts every stranded block below the floor. Refusing to serve what sits below
	// it is the layer above's job, and is covered by its own contract suite.
	require.Equal(t, uint64(5), db3.GetPruneWatermark(), "recovered watermark must be the lowest surviving QC's first")

	// Everything the surviving QCs cover is still on disk.
	for n := uint64(5); n < 20; n++ {
		require.True(t, physicallyPresent(t, db3, blocktypes.KindBlock, n), "block %d must survive", n)
		require.True(t, physicallyPresent(t, db3, blocktypes.KindQC, n), "QC key %d must survive", n)
	}
}

// TestReclaimsAcrossRestart verifies the durable reclamation path: records
// written, then left below the floor after a restart (which seals the segments
// they landed in), are physically collected by GC.
//
// The floor sits at 15, the newest cohort's start, which is where the layer above
// caps a prune past the head. Blocks 15..19 and QC[15,20) stay live, pinning
// their segments against GC, while every fully-below segment — blocks 0..14 and
// QCs [0,5),[5,10),[10,15) — is reclaimed.
func TestReclaimsAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, strandingConfig(t, dir, 8))
	blocktest.WriteCohorts(t, db, 4, 5) // blocks 0..19
	blocktest.WriteAppData(t, db, 4, 5)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Reopen: the segments written above are now sealed and collectable.
	db2 := openLitt(t, strandingConfig(t, dir, 8))
	defer func() { _ = db2.Close() }()

	db2.SetPruneWatermark(15)
	require.NoError(t, littblock.ForceGC(db2))

	for n := uint64(0); n < 15; n++ {
		require.False(t, physicallyPresent(t, db2, blocktypes.KindBlock, n), "block %d must be reclaimed", n)
		require.False(t, physicallyPresent(t, db2, blocktypes.KindQC, n), "QC key %d must be reclaimed", n)
	}
	for n := uint64(15); n < 20; n++ {
		require.True(t, physicallyPresent(t, db2, blocktypes.KindBlock, n), "block %d must survive the capped prune", n)
	}
	require.True(t, physicallyPresent(t, db2, blocktypes.KindQC, 15), "covering QC[15,20) must survive the capped prune")
}

// TestPruneIntoCohortRetainsTheWholeCohort pins cohort atomicity on disk. It is
// why the layer above rounds a prune down to a cohort start: with the floor on
// that boundary, and the QC written before the blocks it covers, pinning the QC's
// segment pins every segment after it, so the whole covered range is retained
// rather than split.
//
// The test is written so it would fail if the rule broke. It first asserts the
// fully-below segment IS reclaimed: LittDB never collects the live mutable file,
// so this is what proves GC ran with teeth on this data, without which the
// retention assertions below would hold vacuously. The write, flush, close and
// reopen dance seals the mutable file so GC can reach it.
//
// Layout with MaxSegmentKeyCount = 8: seg0 = {QC[0,5), b0, b1}, seg1 = {b2, b3,
// b4, QC[5,10)}, seg2 = {b5, b6, b7, b8}, seg3 = {b9, ...}. With the floor at 5,
// seg0 falls entirely below it and is reclaimed, while QC[5,10)'s key 5 pins seg1
// and everything after it.
func TestPruneIntoCohortRetainsTheWholeCohort(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, strandingConfig(t, dir, 8))
	blocktest.WriteCohorts(t, db, 4, 5) // blocks 0..19; QC[5,10) covers blocks 5..9
	blocktest.WriteAppData(t, db, 4, 5)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	db2 := openLitt(t, strandingConfig(t, dir, 8))
	defer func() { _ = db2.Close() }()

	db2.SetPruneWatermark(5)
	require.NoError(t, littblock.ForceGC(db2))

	// GC had teeth: the fully-below segment {QC[0,5), b0, b1} is gone.
	require.False(t, physicallyPresent(t, db2, blocktypes.KindBlock, 0), "block 0 is fully below the watermark")
	require.False(t, physicallyPresent(t, db2, blocktypes.KindBlock, 1), "block 1 is fully below the watermark")
	require.False(t, physicallyPresent(t, db2, blocktypes.KindQC, 0), "QC[0,5) is entirely below the watermark")

	// The cohort's QC is at the watermark, so its segment is pinned — by its
	// primary key and by every alias across its covered range.
	require.True(t, physicallyPresent(t, db2, blocktypes.KindQC, 5), "cohort QC[5,10) primary key must survive")
	require.True(t, physicallyPresent(t, db2, blocktypes.KindQC, 9), "cohort QC[5,10) alias must survive")

	// Pinning the QC's segment retains the whole range it covers.
	for n := uint64(5); n < 10; n++ {
		require.True(t, physicallyPresent(t, db2, blocktypes.KindBlock, n),
			"block %d in the cohort must be retained on disk", n)
	}
}

// TestRefusesToOpenWithStrandedBlocks verifies the corruption guard in watermark
// recovery. The never-empty prune rule guarantees at least one cohort is always
// retained, so a store holding records but no QC is corrupt — a QC file removed
// out of band, say. Rather than serve records it can no longer place, the store
// refuses to open.
func TestRefusesToOpenWithStrandedBlocks(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, strandingConfig(t, dir, 8))
	require.NoError(t, db.PutBlock(5, blocktest.BlockHash(5), blocktest.RecordValue(blocktypes.KindBlock, 5)))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	_, err := littblock.NewBlockDB(strandingConfig(t, dir, 8))
	require.ErrorContains(t, err, "no QC in non-empty store")
}
