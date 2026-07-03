package hashlog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A logger opened with no caller types can have its columns populated via RegisterHashType, and the
// registered columns are recorded.
func TestRegisterHashTypePopulatesColumns(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil                 // start empty; populate via RegisterHashType
	config.DisableChangesetHashing = false // changeset column makes an empty caller set valid (real usage)
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	require.NoError(t, l.RegisterHashType("a"))
	require.NoError(t, l.RegisterHashType("b"))
	require.NoError(t, l.RegisterHashType("a")) // idempotent

	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	l.ReportChangeset(1, nil) // complete the changeset column with a nil hash
	require.NoError(t, l.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, []byte{0x01}, logs[0].Hashes["a"])
	require.Equal(t, []byte{0x02}, logs[0].Hashes["b"])
}

// RegisterHashType rejects the reserved changeset column and illegal names.
func TestRegisterHashTypeRejectsReservedAndIllegal(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false // changeset column is active, so the name is reserved
	l, err := NewHashLogger(config)
	require.NoError(t, err)
	defer func() { require.NoError(t, l.Close()) }()

	require.Error(t, l.RegisterHashType(ChangesetHashType))
	require.Error(t, l.UnregisterHashType(ChangesetHashType))
	require.Error(t, l.RegisterHashType("bad,name"))
	require.NoError(t, l.RegisterHashType("memIAVL/mod/bank"))
}

// Changing the column set after blocks have been logged seals the current file and starts a new one
// whose header reflects the new columns. Blocks logged before and after the change read back correctly.
func TestColumnChangeRotatesFile(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir) // changeset hashing disabled; caller types only
	config.HashTypes = []string{"a"}
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	// Block 1 with only column "a".
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))

	// Add column "b" mid-run, then log block 2 with both columns.
	require.NoError(t, l.RegisterHashType("b"))
	require.NoError(t, l.ReportHash(2, "a", []byte{0x02}))
	require.NoError(t, l.ReportHash(2, "b", []byte{0x12}))

	// Remove column "a", then log block 3 with only "b".
	require.NoError(t, l.UnregisterHashType("a"))
	require.NoError(t, l.ReportHash(3, "b", []byte{0x13}))

	require.NoError(t, l.Close())

	// More than one file should exist (the set changed twice).
	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.Greater(t, len(files), 1, "a column change should have rotated to a new file")

	byBlock := map[uint64]HashLog{}
	for _, log := range readAllLogs(t, dir) {
		byBlock[log.BlockNumber] = log
	}
	require.Len(t, byBlock, 3)
	require.Equal(t, []byte{0x01}, byBlock[1].Hashes["a"])
	require.Equal(t, []byte{0x02}, byBlock[2].Hashes["a"])
	require.Equal(t, []byte{0x12}, byBlock[2].Hashes["b"])
	require.Equal(t, []byte{0x13}, byBlock[3].Hashes["b"])
	// Block 3's file no longer carries column "a".
	_, hasA := byBlock[3].Hashes["a"]
	require.False(t, hasA, "column a should be gone from block 3")
}

// A burst of column registrations before any block is written leaves no orphan files: the empty
// intermediate files are removed as each rotation seals them.
func TestColumnChangeBurstLeavesNoOrphans(t *testing.T) {
	dir := t.TempDir()
	config := testConfig(dir)
	config.HashTypes = nil
	config.DisableChangesetHashing = false
	l, err := NewHashLogger(config)
	require.NoError(t, err)

	for _, name := range []string{"a", "b", "c", "d"} {
		require.NoError(t, l.RegisterHashType(name))
	}
	require.NoError(t, l.ReportHash(1, "a", []byte{0x01}))
	require.NoError(t, l.ReportHash(1, "b", []byte{0x02}))
	require.NoError(t, l.ReportHash(1, "c", []byte{0x03}))
	require.NoError(t, l.ReportHash(1, "d", []byte{0x04}))
	l.ReportChangeset(1, nil)
	require.NoError(t, l.Close())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1, "intermediate empty files should have been removed")

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, []byte{0x04}, logs[0].Hashes["d"])
}
