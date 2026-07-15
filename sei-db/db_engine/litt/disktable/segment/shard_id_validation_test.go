package segment

import (
	"log/slog"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// TestSegmentReadRejectsOutOfRangeShardID is a sanity check that a forged Address with a shard ID
// outside the segment's sharding factor causes Segment.Read to return an error rather than indexing
// out of bounds and panicking.
func TestSegmentReadRejectsOutOfRangeShardID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	const shardingFactor uint8 = 4

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	require.NoError(t, segmentPath.MakeDirectories(false))

	seg, err := CreateSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		rand.Uint32(),
		[]*SegmentPath{segmentPath},
		false,
		shardingFactor,
		types.CompressionNone,
		false,
		32)
	require.NoError(t, err)

	badAddr := types.NewAddress(seg.SegmentIndex(), 0, shardingFactor, 0)

	require.NotPanics(t, func() {
		_, err := seg.Read([]byte("does-not-matter"), badAddr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "out of range")
	})

	// Wind down the segment's control-loop goroutines before the test exits.
	_, err = seg.Seal(time.Now())
	require.NoError(t, err)
}
