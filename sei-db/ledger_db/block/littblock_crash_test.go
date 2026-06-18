package block

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
)

// TestLittblockNoBlockWithoutQCAfterTornTail is the end-to-end proof of the
// headline crash invariant: after a torn write, every persisted block is still
// covered by a persisted QC (a persisted QC may lack some of its blocks, never
// the reverse). It writes full QC-then-blocks batches into the single
// single-shard ledger table, then physically truncates the tail of the segment's
// value file (dropping the last-written block bytes) and marks the segment
// unsealed so reopening runs litt's group-atomic recovery — which keeps a
// contiguous write-order prefix. Because every covering QC is written before its
// blocks, that prefix can never contain a block whose QC was dropped.
//
// The segment-level TestSealLoadedSegmentSingleShardPrefix proves the underlying
// single-shard prefix property in isolation; this pins the block-store behavior
// that depends on it (one shared table, QC-before-block ordering).
func TestLittblockNoBlockWithoutQCAfterTornTail(t *testing.T) {
	dir := t.TempDir()
	committee, keys := buildCommittee()
	batches := generateBatches(committee, keys)

	db, err := littblock.NewBlockDB(littConfig(t, dir))
	require.NoError(t, err)
	writeAll(t, db, batches)
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	// Corrupt the segment holding the most value bytes (where the most recent
	// writes landed): drop the tail of its value file, then flip its metadata
	// back to unsealed so LoadSegment re-runs recovery on reopen.
	valPath, metaPath := largestValueSegmentFiles(t, dir)
	truncateFileBy(t, valPath, 16)
	markSegmentUnsealedOnDisk(t, metaPath)

	// Reopen: recovery discards the torn tail, keeping a contiguous prefix.
	db2, err := littblock.NewBlockDB(littConfig(t, dir))
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()

	totalBlocks := 0
	for _, b := range batches {
		totalBlocks += len(b.blocks)
	}

	it, err := db2.Blocks(false)
	require.NoError(t, err)
	defer func() { _ = it.Close() }()
	present := 0
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		n := it.Number()
		qc, err := db2.ReadQCByBlockNumber(n)
		require.NoError(t, err)
		require.True(t, qc.IsPresent(), "block %d survived but its covering QC was lost", n)
		present++
	}

	// The truncation must have actually dropped at least one block, otherwise the
	// recovery path was never exercised and the invariant proves nothing.
	require.Less(t, present, totalBlocks, "expected the torn tail to drop at least one block")
}

// largestValueSegmentFiles walks the litt data directory under dir and returns
// the value-file and sibling metadata-file paths of the segment with the most
// value bytes (the one most recently written into; robust to empty rollover
// segments that may exist after a clean Close).
func largestValueSegmentFiles(t *testing.T, dir string) (valPath string, metaPath string) {
	t.Helper()
	var bestSize int64 = -1
	var bestIndex string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), segment.ValuesFileExtension) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > bestSize {
			// File name is "<index>-<shard>.values"; the index is everything
			// before the dash.
			base := strings.TrimSuffix(d.Name(), segment.ValuesFileExtension)
			index := base
			if i := strings.IndexByte(base, '-'); i >= 0 {
				index = base[:i]
			}
			bestSize = info.Size()
			bestIndex = index
			valPath = p
		}
		return nil
	}))
	require.NotEmpty(t, valPath, "no value file found under %s", dir)
	_, err := strconv.ParseUint(bestIndex, 10, 32)
	require.NoError(t, err, "unexpected segment index %q", bestIndex)
	metaPath = filepath.Join(filepath.Dir(valPath), bestIndex+segment.MetadataFileExtension)
	return valPath, metaPath
}

// truncateFileBy drops the last n bytes of the file at p.
func truncateFileBy(t *testing.T, p string, n int) {
	t.Helper()
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Greater(t, len(data), n, "file %s too small to truncate by %d", p, n)
	require.NoError(t, os.WriteFile(p, data[:len(data)-n], 0600))
}

// markSegmentUnsealedOnDisk flips the sealed byte in a segment's metadata file
// from 1 back to 0, simulating a segment that crashed before sealing so that
// LoadSegment runs the recovery path on reopen.
func markSegmentUnsealedOnDisk(t *testing.T, metaPath string) {
	t.Helper()
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Equal(t, segment.V3MetadataSize, len(data), "unexpected metadata size for %s", metaPath)
	data[segment.V3MetadataSize-1] = 0
	require.NoError(t, os.WriteFile(metaPath, data, 0600))
}
