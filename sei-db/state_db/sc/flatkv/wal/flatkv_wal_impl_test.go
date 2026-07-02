package wal

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func testConfig(dir string) *FlatKVWALConfig {
	return DefaultFlatKVWALConfig(dir)
}

func openWAL(t *testing.T, cfg *FlatKVWALConfig) FlatKVWAL {
	t.Helper()
	w, err := NewFlatKVWAL(cfg)
	require.NoError(t, err)
	return w
}

// writeBlock writes a single changeset for the block and signals end of block.
func writeBlock(t *testing.T, w FlatKVWAL, block uint64) {
	t.Helper()
	cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte{byte(block)}, []byte{byte(block)})}
	require.NoError(t, w.Write(block, cs))
	require.NoError(t, w.SignalEndOfBlock())
}

// collectBlocks iterates from start and returns the block number of each coalesced block entry, verifying
// that entries are strictly increasing and never carry an end-of-block marker.
func collectBlocks(t *testing.T, w FlatKVWAL, start uint64) []uint64 {
	t.Helper()
	it, err := w.Iterator(start)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var blocks []uint64
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		entry := it.Entry()
		require.GreaterOrEqual(t, entry.BlockNumber, start)
		require.False(t, entry.EndOfBlock)
		if len(blocks) > 0 {
			require.Greater(t, entry.BlockNumber, blocks[len(blocks)-1])
		}
		blocks = append(blocks, entry.BlockNumber)
	}
	return blocks
}

func TestWriteFlushReopenGetRange(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(5), end)
	require.NoError(t, w.Close())

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, start, end, err = w2.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(5), end)

	require.Equal(t, []uint64{1, 2, 3, 4, 5}, collectBlocks(t, w2, 1))
}

func TestContractViolations(t *testing.T) {
	t.Run("block numbers may not decrease", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		writeBlock(t, w, 5)
		require.Error(t, w.Write(4, nil))
	})

	t.Run("cannot advance block without ending the previous one", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.NoError(t, w.Write(1, nil))
		require.Error(t, w.Write(2, nil))
	})

	t.Run("cannot write to an ended block", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.NoError(t, w.Write(1, nil))
		require.NoError(t, w.SignalEndOfBlock())
		require.Error(t, w.Write(1, nil))
	})

	t.Run("end of block with no block in progress is an error", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.Error(t, w.SignalEndOfBlock())
	})

	t.Run("multiple writes to the same block are allowed before end of block", func(t *testing.T) {
		w := openWAL(t, testConfig(t.TempDir()))
		defer func() { require.NoError(t, w.Close()) }()
		require.NoError(t, w.Write(1, []*proto.NamedChangeSet{makeChangeSet("a", []byte("k1"), []byte("v1"))}))
		require.NoError(t, w.Write(1, []*proto.NamedChangeSet{makeChangeSet("b", []byte("k2"), []byte("v2"))}))
		require.NoError(t, w.SignalEndOfBlock())
	})
}

func TestIncompleteTailBlockDiscardedOnReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	// Block 4 is written but never ended (a crash mid-block).
	require.NoError(t, w.Write(4, []*proto.NamedChangeSet{makeChangeSet("evm", []byte{0x04}, []byte{0x04})}))
	require.NoError(t, w.Flush())
	require.NoError(t, w.Close())

	w2 := openWAL(t, cfg)
	defer func() { require.NoError(t, w2.Close()) }()

	ok, start, end, err := w2.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(3), end)
	require.Equal(t, []uint64{1, 2, 3}, collectBlocks(t, w2, 1))

	// Block 4 may now be re-executed cleanly.
	writeBlock(t, w2, 4)
	require.NoError(t, w2.Flush())
	ok, _, end, err = w2.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(4), end)
}

func TestOrphanFileRecovery(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	// Fabricate an orphaned unsealed file: blocks 1 and 2 complete, block 3 incomplete, left unsealed as if
	// the process crashed before it could seal.
	f, err := newWalFile(dir, 0)
	require.NoError(t, err)
	writeCompleteBlock(t, f, 1)
	writeCompleteBlock(t, f, 2)
	cs := []*proto.NamedChangeSet{makeChangeSet("a", []byte{3}, []byte{3})}
	require.NoError(t, f.writeEntry(NewFlatKVWalEntry(3, cs))) // no end-of-block marker: block 3 is incomplete
	require.NoError(t, f.flush(true))
	require.NoError(t, f.file.Close())

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(2), end)
	require.Equal(t, []uint64{1, 2}, collectBlocks(t, w, 1))
}

func TestRotationProducesContiguousSealedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // rotate after every completed block

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 6; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(6), end)
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, collectBlocks(t, w, 1))
	require.NoError(t, w.Close())

	// Every completed block should have produced its own sealed file with a clean [k,k] range.
	var sealed []parsedFileName
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if parsed, okName := parseFileName(entry.Name()); okName && parsed.sealed {
			sealed = append(sealed, parsed)
			require.Equal(t, parsed.firstBlock, parsed.lastBlock)
		}
	}
	require.Len(t, sealed, 6)
}

func countSealedFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	count := 0
	for _, entry := range entries {
		if parsed, ok := parseFileName(entry.Name()); ok && parsed.sealed {
			count++
		}
	}
	return count
}

func TestBlockNeverSplitAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 128 // tiny, so a single block's data dwarfs the rotation threshold

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()

	// Write many changesets for the same block, far exceeding TargetFileSize, without ending the block.
	const changesetCount = 50
	value := make([]byte, 100)
	for i := 0; i < changesetCount; i++ {
		cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte{byte(i)}, value)}
		require.NoError(t, w.Write(1, cs))
	}
	require.NoError(t, w.Flush())

	// Despite blowing past TargetFileSize many times over, the still-open block must not have been sealed:
	// no sealed file exists yet, so all of block 1's data lives in the single mutable file.
	require.Equal(t, 0, countSealedFiles(t, dir))

	// Closing the block permits rotation; block 1's data is sealed into exactly one file.
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	require.Equal(t, 1, countSealedFiles(t, dir))

	// The iterator coalesces all of block 1's Write records into a single entry whose changeset is the
	// concatenation, in write order, of every record's changesets.
	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	entry := it.Entry()
	require.Equal(t, uint64(1), entry.BlockNumber)
	require.False(t, entry.EndOfBlock)
	require.Len(t, entry.Changeset, changesetCount)
	for i, ncs := range entry.Changeset {
		require.Equal(t, []byte{byte(i)}, ncs.Changeset.Pairs[0].Key)
	}

	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestPruneDropsWholeFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one block per file, so pruning can drop whole files

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.NoError(t, w.Prune(5))

	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), start)
	require.Equal(t, uint64(10), end)
	require.Equal(t, []uint64{5, 6, 7, 8, 9, 10}, collectBlocks(t, w, 0))
}

func TestPrunePastAllBlocksEmptiesRange(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one block per file so every block sits in a prunable sealed file

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 5; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	require.NoError(t, w.Prune(100))

	ok, _, _, err := w.GetStoredRange()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestActiveIteratorBlocksPruningOfNeededFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one block per sealed file, so pruning works file-by-file

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	// Hold an iterator anchored at block 1 (the oldest). Its read lease must keep block 1's file alive.
	it, err := w.Iterator(1)
	require.NoError(t, err)

	require.NoError(t, w.Prune(5))
	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start, "block 1 must survive pruning while a live iterator pins it")
	require.Equal(t, uint64(10), end)

	// The iterator still sees the full, intact sequence.
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, collectBlocks(t, w, 1))

	// Releasing the lease lets the same prune make progress.
	require.NoError(t, it.Close())
	require.NoError(t, w.Prune(5))
	ok, start, _, err = w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), start)
}

func TestIteratorAnchoredAboveKeepPointDoesNotBlockPruning(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1

	w := openWAL(t, cfg)
	defer func() { require.NoError(t, w.Close()) }()
	for block := uint64(1); block <= 10; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Flush())

	// An iterator anchored at block 8 does not need blocks below 5, so pruning to 5 proceeds.
	it, err := w.Iterator(8)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	require.NoError(t, w.Prune(5))
	ok, start, _, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), start)
}

func TestScanRejectsGapInSealedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.TargetFileSize = 1 // one block per sealed file

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 4; block++ {
		writeBlock(t, w, block)
	}
	require.NoError(t, w.Close())

	// Delete a middle sealed file to punch a gap in the index sequence, simulating corruption.
	var sealed []parsedFileName
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if p, ok := parseFileName(entry.Name()); ok && p.sealed {
			sealed = append(sealed, p)
		}
	}
	require.GreaterOrEqual(t, len(sealed), 3)
	sort.Slice(sealed, func(i int, j int) bool { return sealed[i].index < sealed[j].index })
	victim := sealed[len(sealed)/2]
	require.NoError(t, os.Remove(filepath.Join(dir, sealedFileName(victim.index, victim.firstBlock, victim.lastBlock))))

	_, err = NewFlatKVWAL(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not contiguous")
}

func TestGetStoredRangeEmpty(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	ok, _, _, err := w.GetStoredRange()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRollbackConstructor(t *testing.T) {
	t.Run("drops whole files beyond the rollback point", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		cfg.TargetFileSize = 1 // one block per file

		w := openWAL(t, cfg)
		for block := uint64(1); block <= 6; block++ {
			writeBlock(t, w, block)
		}
		require.NoError(t, w.Close())

		w2, err := NewFlatKVWALWithRollback(cfg, 3)
		require.NoError(t, err)
		defer func() { require.NoError(t, w2.Close()) }()

		ok, start, end, err := w2.GetStoredRange()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(1), start)
		require.Equal(t, uint64(3), end)
		require.Equal(t, []uint64{1, 2, 3}, collectBlocks(t, w2, 1))
	})

	t.Run("truncates within a file at the rollback point", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir) // large target: all blocks land in one file

		w := openWAL(t, cfg)
		for block := uint64(1); block <= 6; block++ {
			writeBlock(t, w, block)
		}
		require.NoError(t, w.Close())

		w2, err := NewFlatKVWALWithRollback(cfg, 3)
		require.NoError(t, err)
		defer func() { require.NoError(t, w2.Close()) }()

		ok, start, end, err := w2.GetStoredRange()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(1), start)
		require.Equal(t, uint64(3), end)
		require.Equal(t, []uint64{1, 2, 3}, collectBlocks(t, w2, 1))

		// Writing continues cleanly after the rollback point.
		writeBlock(t, w2, 4)
		require.NoError(t, w2.Flush())
		_, _, end, err = w2.GetStoredRange()
		require.NoError(t, err)
		require.Equal(t, uint64(4), end)
	})
}
