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
// white-box check on the raw table: data written, then pruned past after a
// restart (which seals the segments it landed in), is physically collected by GC.
// A raw-table check is required because the read watermark now refuses
// below-watermark reads regardless of physical reclamation, so public reads alone
// can no longer distinguish "reclaimed" from "gated".
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

	require.NoError(t, db2.PruneBefore(20)) // past every block
	require.NoError(t, ForceGC(db2))

	// Every block and QC is physically gone from the raw table.
	for n := types.GlobalBlockNumber(0); n < 20; n++ {
		require.False(t, physicallyPresent(t, impl, blockKey(n)), "block %d must be reclaimed", n)
		require.False(t, physicallyPresent(t, impl, qcKey(n)), "QC key %d must be reclaimed", n)
	}
}
