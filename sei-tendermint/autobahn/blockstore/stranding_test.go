package blockstore_test

import (
	"math"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore/internal/blocktest"
	"github.com/stretchr/testify/require"
)

// strandingConfig builds a config whose only segment-rollover trigger is a small
// MaxSegmentKeyCount, so a test can place a segment boundary at a precise key.
// Retention is tiny (the prune watermark is the sole reclamation gate) and GC is
// effectively background-disabled so ForceGC is the only thing that reclaims.
func strandingConfig(t *testing.T, dir string, maxSegmentKeyCount uint32) *blockstore.Config {
	t.Helper()
	cfg, err := blockstore.DefaultConfig(dir)
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
func physicallyPresent(t *testing.T, get func(types.RoadIndex) ([]byte, bool, error), n types.RoadIndex) bool {
	t.Helper()
	_, exists, err := get(n)
	require.NoError(t, err)
	return exists
}

// TestRecoveredWatermarkTracksOldestBlock verifies the retained floor after GC and restart.
func TestRecoveredWatermarkTracksOldestBlock(t *testing.T) {
	dir := t.TempDir()
	cfg := strandingConfig(t, dir, 8)
	db := openLitt(t, cfg)
	blocktest.WriteCohorts(t, db, 4, 5)
	blocktest.WriteAppData(t, db, 4)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = openLitt(t, cfg)
	db.SetPruneWatermark(1)
	require.NoError(t, db.ForceGC())
	require.NoError(t, db.Close())
	db = openLitt(t, cfg)
	defer func() { require.NoError(t, db.Close()) }()
	require.Equal(t, types.RoadIndex(1), db.GetPruneWatermark())
	requireRetainedBlocks(t, db, 1)
}

// TestReclaimsAcrossRestart verifies reclamation of sealed segments below the floor.
func TestReclaimsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := strandingConfig(t, dir, 8)
	db := openLitt(t, cfg)
	blocktest.WriteCohorts(t, db, 4, 5)
	blocktest.WriteAppData(t, db, 4)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	db = openLitt(t, cfg)
	defer func() { require.NoError(t, db.Close()) }()
	db.SetPruneWatermark(3)
	require.NoError(t, db.ForceGC())
	requireRetainedBlocks(t, db, 3)
}

func requireRetainedBlocks(t *testing.T, db blocktest.DB, first types.RoadIndex) {
	t.Helper()
	for n := types.RoadIndex(0); n < 4; n++ {
		want := n >= first
		record, present, err := db.GetBlockByNumber(n)
		require.NoError(t, err)
		require.Equal(t, want, present)
		if present {
			require.Equal(t, blocktest.BlockFixture(n, 5), record)
		}
		require.Equal(t, want, physicallyPresent(t, db.GetQC, n))
		_, present, err = db.GetBlockByHash(blocktest.BlockHash(n))
		require.NoError(t, err)
		require.Equal(t, want, present)
		for offset := range uint64(5) {
			_, present, err = db.GetLaneBlock(n, offset)
			require.NoError(t, err)
			require.Equal(t, want, present)
		}
	}
}

// TestRefusesToOpenWithoutBlock verifies that app records cannot supply a missing SuperBlock.
func TestRefusesToOpenWithoutBlock(t *testing.T) {
	dir := t.TempDir()
	db := openLitt(t, strandingConfig(t, dir, 8))
	blocktest.PutAppData(t, db, 5)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	_, err := blockstore.OpenRecords(strandingConfig(t, dir, 8))
	require.Error(t, err)
}
