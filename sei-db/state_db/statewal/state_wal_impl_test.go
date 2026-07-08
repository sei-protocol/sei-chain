package statewal

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func testConfig(dir string) *Config {
	return DefaultConfig(dir)
}

func openWAL(t *testing.T, cfg *Config) StateWAL {
	t.Helper()
	w, err := New(cfg)
	require.NoError(t, err)
	return w
}

// writeBlock writes a single changeset for the block and signals end of block.
func writeBlock(t *testing.T, w StateWAL, block uint64) {
	t.Helper()
	cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte{byte(block)}, []byte{byte(block)})}
	require.NoError(t, w.Write(block, cs))
	require.NoError(t, w.SignalEndOfBlock())
}

// collectBlocks iterates from start and returns the block number of each entry, verifying that entries are
// strictly increasing and never below start.
func collectBlocks(t *testing.T, w StateWAL, start uint64) []uint64 {
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

func TestIncompleteBlockDiscardedOnReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	w := openWAL(t, cfg)
	for block := uint64(1); block <= 3; block++ {
		writeBlock(t, w, block)
	}
	// Block 4 is written but never ended (a crash mid-block): it was never appended as a record.
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

func TestGetStoredRangeEmpty(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	ok, _, _, err := w.GetStoredRange()
	require.NoError(t, err)
	require.False(t, ok)
}

func TestEmptyChangesetBlockIsStored(t *testing.T) {
	w := openWAL(t, testConfig(t.TempDir()))
	defer func() { require.NoError(t, w.Close()) }()

	// A block with an empty changeset that is properly ended is a real, stored block.
	require.NoError(t, w.Write(1, []*proto.NamedChangeSet{}))
	require.NoError(t, w.SignalEndOfBlock())
	require.NoError(t, w.Flush())

	ok, start, end, err := w.GetStoredRange()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(1), start)
	require.Equal(t, uint64(1), end)

	it, err := w.Iterator(1)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()
	ok, err = it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	entry := it.Entry()
	require.Equal(t, uint64(1), entry.BlockNumber)
	require.Empty(t, entry.Changeset)
}

func TestPruneDropsOldBlocks(t *testing.T) {
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

// TestRollbackConstructor is a wrapper-level smoke test that NewWithRollback drops blocks beyond the rollback
// point end to end (the crash-safety details are exercised in the seiwal package).
func TestRollbackConstructor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetSize uint
	}{
		{"whole-file removal", 1},                // one block per file: rollback removes whole trailing files
		{"in-file truncation", 64 * 1024 * 1024}, // all blocks in one file: rollback truncates it in place
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := testConfig(dir)
			cfg.TargetFileSize = tc.targetSize

			w := openWAL(t, cfg)
			for block := uint64(1); block <= 6; block++ {
				writeBlock(t, w, block)
			}
			require.NoError(t, w.Close())

			w2, err := NewWithRollback(cfg, 3)
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
}
