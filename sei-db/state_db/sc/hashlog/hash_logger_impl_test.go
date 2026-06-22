package hashlog

import (
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// testConfig returns a config with generous buffers so that, for the modest volumes used in tests, nothing is
// dropped and the pipeline is deterministic once Close() has drained it. Diff hashing is disabled by default so
// blocks complete purely via ReportHash; tests that exercise the diff enable it explicitly.
func testConfig(dir string) *HashLoggerConfig {
	return &HashLoggerConfig{
		Path:              dir,
		Version:           "v1.0.0",
		HashTypes:         []string{"a", "b"},
		DiffHashType:      "",
		HashBufferSize:    8192,
		WriteBufferSize:   8192,
		ControlBufferSize: 8192,
		BlocksToRetain:    1_000_000,
		TargetFileSize:    unit.MB,
		MaxDiskSize:       unit.GB,
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
	// blocking emission of N+1) until its "b" arrives — mirroring a lagging diff hash.
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

func TestImplReportDiffPopulatesDiffHash(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = []string{"diff"}
	config.DiffHashType = "diff"
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	changeSet := []*proto.NamedChangeSet{cs("bank", kv("key", "value"))}
	l.ReportDiff(1, changeSet)
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, hashDiff(changeSet), logs[0].Hashes["diff"])
}

func TestImplReportDiffNilOptsOut(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = []string{"diff"}
	config.DiffHashType = "diff"
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	// A nil change set records a nil diff hash (opting out for this block) rather than hashing an empty diff.
	l.ReportDiff(1, nil)
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(1), logs[0].BlockNumber)
	require.Nil(t, logs[0].Hashes["diff"])
}

func TestImplReportDiffEmptyChangeSetIsHashed(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = []string{"diff"}
	config.DiffHashType = "diff"
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	// An empty (non-nil) change set is a legitimate no-change block: it gets the hash of the empty diff, which
	// is non-nil and deterministic (distinct from the nil opt-out above).
	empty := []*proto.NamedChangeSet{}
	l.ReportDiff(1, empty)
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.NotNil(t, logs[0].Hashes["diff"])
	require.Equal(t, hashDiff(empty), logs[0].Hashes["diff"])
}

func TestImplDiffFloodIsHashedReliably(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = []string{"diff"}
	config.DiffHashType = "diff"
	config.HashBufferSize = 1 // tiny buffer: a flood now backpressures rather than dropping
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	const blocks = 200
	changeSet := []*proto.NamedChangeSet{cs("bank", kv("k", "v"))}
	for block := uint64(1); block <= blocks; block++ {
		l.ReportDiff(block, changeSet)
	}
	require.NoError(t, l.Close())

	// Every block must appear exactly once, and its diff hash must be the real hash — nothing is shed even
	// under a flood through a one-deep hasher channel.
	want := hashDiff(changeSet)
	byBlock := make(map[uint64][]byte)
	for _, log := range readAllLogs(t, dir) {
		_, dup := byBlock[log.BlockNumber]
		require.False(t, dup, "block %d recorded more than once", log.BlockNumber)
		byBlock[log.BlockNumber] = log.Hashes["diff"]
	}
	for block := uint64(1); block <= blocks; block++ {
		require.Equal(t, want, byBlock[block], "block %d missing or has a shed (nil) diff hash", block)
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

func TestImplRollbackOpensNewFile(t *testing.T) {
	dir := t.TempDir()
	l, err := NewHashLogger(testConfig(dir))
	require.NoError(t, err)

	for block := uint64(1); block <= 3; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block)}))
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block)}))
	}
	// Re-execute blocks 1 and 2 (a rollback).
	for block := uint64(1); block <= 2; block++ {
		require.NoError(t, l.ReportHash(block, "a", []byte{byte(block + 50)}))
		require.NoError(t, l.ReportHash(block, "b", []byte{byte(block + 50)}))
	}
	require.NoError(t, l.Close())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 2, "a rollback should start a new file")

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
	config.HashTypes = []string{"diff", "a"}
	config.DiffHashType = "diff"
	l, err := NewHashLogger(config)
	require.NoError(t, err)
	require.NoError(t, l.Close())

	// After Close the pipeline channels are closed; a stray Report* must fail fast rather than panic on a
	// send to a closed channel (or hang).
	require.NotPanics(t, func() {
		l.ReportDiff(1, []*proto.NamedChangeSet{cs("bank", kv("k", "v"))})
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
