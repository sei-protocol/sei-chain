package hashlog

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// testConfig returns a config with generous buffers so that, for the modest volumes used in tests, nothing is
// dropped and the pipeline is deterministic once Close() has drained it. Changeset hashing is disabled by default so
// blocks complete purely via ReportHash; tests that exercise the changeset enable it explicitly.
func testConfig(dir string) *HashLoggerConfig {
	return &HashLoggerConfig{
		Path:                    dir,
		Version:                 "v1.0.0",
		HashTypes:               []string{"a", "b"},
		DisableChangesetHashing: true,
		HashBufferSize:          8192,
		WriteBufferSize:         8192,
		ControlBufferSize:       8192,
		MaxBufferedBlocks:       1 << 20, // large: existing tests never trip the overflow flush
		BlocksToRetain:          1_000_000,
		TargetFileSize:          unit.MB,
		MaxDiskSize:             unit.GB,
	}
}

func readAllLogs(t *testing.T, dir string) []HashLog {
	t.Helper()
	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	var all []HashLog
	for _, f := range files {
		file, err := ReadHashLogFile(filepath.Join(dir, f.name))
		require.NoError(t, err)
		all = append(all, file.logs...)
	}
	return all
}

func TestImplEmitsInBlockOrderDespiteLaggingType(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	// Report type "a" for blocks 1..5 first, then type "b". Block N stays incomplete (and therefore buffered,
	// blocking emission of N+1) until its "b" arrives — mirroring a lagging changeset hash.
	for block := uint64(1); block <= 5; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
	}
	for block := uint64(1); block <= 5; block++ {
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block + 100)}))
	}
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 5)
	for i, log := range logs {
		require.Equal(t, uint64(i+1), log.BlockNumber)
		require.Equal(t, []byte{byte(i + 1)}, log.Hashes["a"])
		require.Equal(t, []byte{byte(i + 101)}, log.Hashes["b"])
	}
}

func TestImplReportHashUnknownType(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)
	defer func() { require.NoError(t, l.Close()) }()

	require.ErrorContains(t, l.ReportHash(1, "nonexistent", []byte{0x01}), "unknown hash type")
}

func TestImplReportHashRejectsReservedChangesetType(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = []string{"a"}
	config.DisableChangesetHashing = false
	l, err := NewHashLogger(config)
	require.NoError(t, err)
	defer func() { require.NoError(t, l.Close()) }()

	// The changeset column is logger-owned; reporting it via ReportHash must be rejected so it cannot clobber or
	// race the internally computed changeset hash.
	err = l.ReportHash(1, "changeset", []byte{0x01})
	require.ErrorContains(t, err, "reserved")

	// A normal caller-reported type is still accepted.
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
}

func TestImplNilHashCompletesBlock(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	require.NoError(t, l.ReportHash(1, "a", nil)) // disabled subsystem
	require.NoError(t, l.ReportHash(1, "b", []byte{0x42}))
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Nil(t, logs[0].Hashes["a"])
	require.Equal(t, []byte{0x42}, logs[0].Hashes["b"])
}

func TestImplReportChangesetPopulatesChangesetHash(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	changeSet := []*proto.NamedChangeSet{cs("bank", kv("key", "value"))}
	l.ReportChangeset(1, changeSet)
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, hashChangeset(changeSet), logs[0].Hashes["changeset"])
}

func TestImplReportChangesetNilOptsOut(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	// A nil change set records a nil changeset hash (opting out for this block) rather than hashing an empty changeset.
	l.ReportChangeset(1, nil)
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(1), logs[0].BlockNumber)
	require.Nil(t, logs[0].Hashes["changeset"])
}

func TestImplReportChangesetEmptyChangeSetIsHashed(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	// An empty (non-nil) change set is a legitimate no-change block: it gets the hash of the empty changeset, which
	// is non-nil and deterministic (distinct from the nil opt-out above).
	empty := []*proto.NamedChangeSet{}
	l.ReportChangeset(1, empty)
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.NotNil(t, logs[0].Hashes["changeset"])
	require.Equal(t, hashChangeset(empty), logs[0].Hashes["changeset"])
}

func TestImplChangesetFloodIsHashedReliably(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false
	config.HashBufferSize = 1 // tiny buffer: a flood now backpressures rather than dropping
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	const blocks = 200
	changeSet := []*proto.NamedChangeSet{cs("bank", kv("k", "v"))}
	for block := uint64(1); block <= blocks; block++ {
		l.ReportChangeset(block, changeSet)
	}
	require.NoError(t, l.Close())

	// Every block must appear exactly once, and its changeset hash must be the real hash — nothing is shed even
	// under a flood through a one-deep hasher channel.
	want := hashChangeset(changeSet)
	byBlock := make(map[uint64][]byte)
	for _, log := range readAllLogs(t, dir) {
		_, dup := byBlock[log.BlockNumber]
		require.False(t, dup, "block %d recorded more than once", log.BlockNumber)
		byBlock[log.BlockNumber] = log.Hashes["changeset"]
	}
	for block := uint64(1); block <= blocks; block++ {
		require.Equal(t, want, byBlock[block], "block %d missing or has a shed (nil) changeset hash", block)
	}
}

func TestImplFileRotation(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.TargetFileSize = 1 // rotate after every record
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	for block := uint64(1); block <= 3; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block)}))
	}
	require.NoError(t, l.Close())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 3, "each block should land in its own file")
	for _, f := range files {
		require.True(t, f.parsed.sealed, "no unsealed files should remain after a clean Close")
	}
}

func TestImplRollbackViaReopen(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	for block := uint64(1); block <= 3; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block)}))
	}
	// To roll back, close the logger and open a new one, then re-execute blocks 1 and 2. The reopened logger
	// starts with nothing flushed, so the re-executed blocks are logged into a fresh file rather than discarded.
	require.NoError(t, l.Close())

	l2, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)
	for block := uint64(1); block <= 2; block++ {
		require.NoError(t, l2.ReportHash(block, "a", []byte{byte(block + 50)}))
		require.NoError(t, l2.ReportHash(block, "b", []byte{byte(block + 50)}))
	}
	require.NoError(t, l2.Close())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 2, "reopening after a rollback should start a new file")

	block1, err := ReadHashForBlock(dir, 1)
	require.NoError(t, err)
	require.Len(t, block1, 2, "block 1 was executed twice")
}

func TestImplCleanCloseLeavesNoUnsealedFile(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	require.NoError(t, l.Close())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, files[0].parsed.sealed)
}

func TestImplReportAfterCloseFailsFast(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = []string{"a"}
	config.DisableChangesetHashing = false
	l, err := NewHashLogger(config)
	require.NoError(t, err)
	require.NoError(t, l.Close())

	// After Close the pipeline channels are closed; a stray Report* must fail fast rather than panic on a
	// send to a closed channel (or hang).
	require.NotPanics(t, func() {
		l.ReportChangeset(1, []*proto.NamedChangeSet{cs("bank", kv("k", "v"))})
		require.Error(t, l.ReportHash(1, "a", []byte{0x01}))
	})
}

func TestImplCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	require.NoError(t, l.Close())
	require.NoError(t, l.Close())
}

func TestImplGCHonorsBlocksToRetain(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.TargetFileSize = 1 // one block per file
	config.BlocksToRetain = 2
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	for block := uint64(1); block <= 5; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block)}))
	}
	require.NoError(t, l.Close())

	gone, err := ReadHashForBlock(dir, 1)
	require.NoError(t, err)
	require.Empty(t, gone, "block 1 should have been garbage collected")

	kept, err := ReadHashForBlock(dir, 5)
	require.NoError(t, err)
	require.Len(t, kept, 1, "the most recent block should be retained")
}

func TestImplGCHonorsMaxDiskSize(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.TargetFileSize = 1 // one block per file
	config.BlocksToRetain = 1_000_000
	config.MaxDiskSize = 100 // bytes; far smaller than 20 files' worth
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	const blocks = 20
	for block := uint64(1); block <= blocks; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block)}))
	}
	require.NoError(t, l.Close())

	gone, err := ReadHashForBlock(dir, 1)
	require.NoError(t, err)
	require.Empty(t, gone, "oldest block should be evicted by the disk-size cap")

	kept, err := ReadHashForBlock(dir, blocks)
	require.NoError(t, err)
	require.Len(t, kept, 1, "the most recent block should be retained")
}

func TestImplResumesAfterReopen(t *testing.T) {
	dir := t.TempDir()

	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	require.NoError(t, l.Close())

	// Reopen the same directory and write another block; the prior data must still be readable.
	l2, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)
	require.NoError(t, l2.ReportHash(2, "a", []byte{0x03}))
	require.NoError(t, l2.ReportHash(2, "b", []byte{0x04}))
	require.NoError(t, l2.Close())

	require.Len(t, readAllLogs(t, dir), 2)
}

func TestImplOverflowFlushesOldestIncomplete(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.MaxBufferedBlocks = 3
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	// Report only type "a" for blocks 1..10. None can complete (each is missing "b"), so without an overflow
	// bound they would buffer forever. With a bound of 3, the oldest incomplete blocks are force-flushed.
	for block := uint64(1); block <= 10; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
	}
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	// At least the blocks beyond the buffer bound must have been forced to disk, incomplete.
	require.GreaterOrEqual(t, len(logs), 7, "oldest incomplete blocks should be force-flushed")
	for _, log := range logs {
		require.Equal(t, []byte{byte(log.BlockNumber)}, log.Hashes["a"])
		require.Nil(t, log.Hashes["b"], "block %d was flushed incomplete, so b must be nil", log.BlockNumber)
	}
}

func TestImplOverflowNeverFlushesBlockAwaitingChangeset(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false
	config.HashBufferSize = 1    // tiny hasher channel: changesets back up and stay in flight
	config.MaxBufferedBlocks = 2 // tight buffer bound, pressuring the overflow path
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	const blocks = 300
	changeSet := []*proto.NamedChangeSet{cs("bank", kv("k", "v"))}
	for block := uint64(1); block <= blocks; block++ {
		l.ReportChangeset(block, changeSet)
	}
	require.NoError(t, l.Close())

	// Every block must be present with its real changeset hash. If the overflow path ever force-flushed a block whose
	// changeset was still in flight, that block would have a nil changeset here.
	want := hashChangeset(changeSet)
	byBlock := make(map[uint64][]byte)
	for _, log := range readAllLogs(t, dir) {
		_, dup := byBlock[log.BlockNumber]
		require.False(t, dup, "block %d recorded more than once", log.BlockNumber)
		byBlock[log.BlockNumber] = log.Hashes["changeset"]
	}
	for block := uint64(1); block <= blocks; block++ {
		require.Equal(t, want, byBlock[block], "block %d missing or force-flushed before its changeset arrived", block)
	}
}

func TestImplCleanCloseDiscardsIncompleteBlocks(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	// Block 1 complete, block 2 incomplete (missing "b"), block 3 complete. A clean close must write the two
	// complete blocks (even across the gap left by block 2) and drop the incomplete one rather than persist a
	// partial record that would read back as a spurious divergence.
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	require.NoError(t, l.ReportHash(2, "a", []byte{0x03}))
	require.NoError(t, l.ReportHash(3, "a", []byte{0x04}))
	require.NoError(t, l.ReportHash(3, "b", []byte{0x05}))
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 2, "only the two complete blocks should be written")
	require.Equal(t, uint64(1), logs[0].BlockNumber)
	require.Equal(t, uint64(3), logs[1].BlockNumber)

	gone, err := ReadHashForBlock(dir, 2)
	require.NoError(t, err)
	require.Empty(t, gone, "an incomplete block must be discarded at clean close, not written as a partial record")
}

func TestImplDiscardsReportsForFlushedBlocksWithoutReopen(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	// Complete and flush block 1.
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	// Now report block 1 again without reopening the logger: these are discarded (already on disk). A genuine
	// rollback would close the logger and reopen it (see TestImplRollbackViaReopen).
	require.NoError(t, l.ReportHash(1, "a", []byte{0x99}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x99}))
	require.NoError(t, l.Close())

	block1, err := ReadHashForBlock(dir, 1)
	require.NoError(t, err)
	require.Len(t, block1, 1, "reports for an already-flushed block must be discarded without reopening")
	require.Equal(t, []byte{0x01}, block1[0].Hashes["a"])
}

func TestImplConcurrentReportAndCloseDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	// Reporters race Close. Because controlChan is never closed (Close uses a ctrlClose sentinel and the
	// control loop cancels senderCtx), a send concurrent with Close must neither panic on a closed channel nor
	// deadlock — it either lands or aborts via senderCtx, and the reporter stops once it observes closed.
	var panicked atomic.Bool
	var wg sync.WaitGroup
	const reporters = 8
	for r := 0; r < reporters; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					panicked.Store(true)
				}
			}()
			for block := uint64(1); ; block++ {
				if err := l.ReportHash(block, "a", []byte{0x01}); err != nil {
					return // logger closed; stop
				}
				_ = l.ReportHash(block, "b", []byte{0x02})
			}
		}()
	}

	require.NoError(t, l.Close())
	wg.Wait() // must return: reporters cannot deadlock after Close
	require.False(t, panicked.Load(), "a Report* racing Close must not panic")
}
