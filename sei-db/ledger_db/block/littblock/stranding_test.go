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
func physicallyPresent(t *testing.T, db blocktypes.BlockDB, kind blocktypes.RecordKind, n blocktypes.BlockNumber) bool {
	t.Helper()
	_, exists, err := blocktest.ReadFixture(db, kind, n)
	require.NoError(t, err)
	return exists
}

// TestRecoveredWatermarkTracksOldestGroup verifies that restart restores the
// oldest retained group's floor after garbage collection.
func TestRecoveredWatermarkTracksOldestGroup(t *testing.T) {
	dir := t.TempDir()
	cfg := strandingConfig(t, dir, 8)
	db := openLitt(t, cfg)
	blocktest.WriteCohorts(t, db, 4, 5)
	blocktest.WriteAppData(t, db, 4, 5)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = openLitt(t, cfg)
	db.SetPruneWatermark(5)
	require.NoError(t, littblock.ForceGC(db))
	require.NoError(t, db.Close())
	db = openLitt(t, cfg)
	defer func() { require.NoError(t, db.Close()) }()
	require.Equal(t, blocktypes.BlockNumber(5), db.GetPruneWatermark())
	for n := blocktypes.BlockNumber(0); n < 20; n++ {
		for _, kind := range []blocktypes.RecordKind{blocktypes.KindQC, blocktypes.KindBlock} {
			require.Equal(t, n >= 5, physicallyPresent(t, db, kind, n))
		}
		_, present, err := db.GetBlockByHash(blocktest.BlockHash(n))
		require.NoError(t, err)
		require.Equal(t, n >= 5, present)
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

	for n := blocktypes.BlockNumber(0); n < 15; n++ {
		require.False(t, physicallyPresent(t, db2, blocktypes.KindBlock, n), "block %d must be reclaimed", n)
		require.False(t, physicallyPresent(t, db2, blocktypes.KindQC, n), "QC key %d must be reclaimed", n)
	}
	for n := blocktypes.BlockNumber(15); n < 20; n++ {
		require.True(t, physicallyPresent(t, db2, blocktypes.KindBlock, n), "block %d must survive the capped prune", n)
	}
	require.True(t, physicallyPresent(t, db2, blocktypes.KindQC, 15), "covering QC[15,20) must survive the capped prune")
}

// TestPruneIntoCohortRetainsTheWholeCohort verifies that a watermark inside a
// group's range retains every QC and block lookup in that group.
func TestPruneIntoCohortRetainsTheWholeCohort(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, strandingConfig(t, dir, 8))
	blocktest.WriteCohorts(t, db, 4, 5) // blocks 0..19; QC[5,10) covers blocks 5..9
	blocktest.WriteAppData(t, db, 4, 5)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	db2 := openLitt(t, strandingConfig(t, dir, 8))
	defer func() { _ = db2.Close() }()

	db2.SetPruneWatermark(7)
	require.NoError(t, littblock.ForceGC(db2))

	// The first group is fully below the watermark and is reclaimed.
	require.False(t, physicallyPresent(t, db2, blocktypes.KindBlock, 0), "block 0 is fully below the watermark")
	require.False(t, physicallyPresent(t, db2, blocktypes.KindBlock, 1), "block 1 is fully below the watermark")
	require.False(t, physicallyPresent(t, db2, blocktypes.KindQC, 0), "QC[0,5) is entirely below the watermark")

	// The second group straddles the watermark and is retained whole.
	require.True(t, physicallyPresent(t, db2, blocktypes.KindQC, 5), "cohort QC[5,10) lookup must survive")
	require.True(t, physicallyPresent(t, db2, blocktypes.KindQC, 9), "cohort QC[5,10) alias must survive")

	// Pinning the QC's segment retains the whole range it covers.
	for n := blocktypes.BlockNumber(5); n < 10; n++ {
		require.True(t, physicallyPresent(t, db2, blocktypes.KindBlock, n),
			"block %d in the cohort must be retained on disk", n)
	}
}

// TestRefusesToOpenWithoutQCGroup verifies that app records cannot supply a missing QC group.
func TestRefusesToOpenWithoutQCGroup(t *testing.T) {
	dir := t.TempDir()
	db := openLitt(t, strandingConfig(t, dir, 8))
	blocktest.PutAppData(t, db, 5, 8)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	_, err := littblock.NewBlockDB(strandingConfig(t, dir, 8))
	require.Error(t, err)
}
