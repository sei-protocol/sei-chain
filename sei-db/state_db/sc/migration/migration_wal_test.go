package migration

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireEmpty asserts that the WAL reports no durable record.
func requireEmpty(t *testing.T, w *MigrationWAL) {
	t.Helper()
	id, payload, err := w.Latest()
	require.NoError(t, err)
	require.Equal(t, uint64(0), id)
	require.Nil(t, payload)
}

// requireLatest asserts that the WAL's most recent durable record matches
// the expected batch ID and payload.
func requireLatest(t *testing.T, w *MigrationWAL, wantID uint64, wantPayload []byte) {
	t.Helper()
	id, payload, err := w.Latest()
	require.NoError(t, err)
	require.Equal(t, wantID, id)
	require.Equal(t, wantPayload, payload)
}

func TestMigrationWAL_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	requireEmpty(t, w)
}

func TestMigrationWAL_OpenCreatesDir(t *testing.T) {
	parent := t.TempDir()
	nested := filepath.Join(parent, "does", "not", "exist", "yet")

	_, err := OpenMigrationWAL(nested)
	require.NoError(t, err)

	info, err := os.Stat(nested)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

// TestMigrationWAL_OnlyLatestRecordSurvives exercises the core semantic: a
// new Append renders prior records obsolete. Only the most recent record is
// surfaced by Latest, both within a single instance and across reopen.
func TestMigrationWAL_OnlyLatestRecordSurvives(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	require.NoError(t, w.Append(1, []byte("one")))
	require.NoError(t, w.Append(2, []byte("two")))
	require.NoError(t, w.Append(3, []byte("three")))

	requireLatest(t, w, 3, []byte("three"))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	requireLatest(t, w2, 3, []byte("three"))
}

// TestMigrationWAL_AppendRequiresContiguousBatchIDs pins the stricter
// contract: each Append must supply exactly previousBatchID + 1, or exactly
// 1 when the WAL is empty. Gaps, duplicates, and regressions are rejected.
func TestMigrationWAL_AppendRequiresContiguousBatchIDs(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	// First append on an empty WAL must be 1.
	require.Error(t, w.Append(0, []byte("zero")))
	require.Error(t, w.Append(2, []byte("skip-zero")))
	require.Error(t, w.Append(100, []byte("way-ahead")))
	requireEmpty(t, w)

	require.NoError(t, w.Append(1, []byte("one")))

	// Subsequent appends must be exactly prior+1.
	require.Error(t, w.Append(1, []byte("dup")))
	require.Error(t, w.Append(0, []byte("regress")))
	require.Error(t, w.Append(3, []byte("skip")))
	requireLatest(t, w, 1, []byte("one"))

	require.NoError(t, w.Append(2, []byte("two")))
	requireLatest(t, w, 2, []byte("two"))
}

func TestMigrationWAL_AppendRejectsOversizedPayload(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	payload := make([]byte, walMaxPayloadSize+1)
	require.Error(t, w.Append(1, payload))
	requireEmpty(t, w)
}

// TestMigrationWAL_PayloadBoundaries covers the three payload shapes most
// likely to expose off-by-one bugs in the record format: empty, medium, and
// large. Only the latest survives, regardless of shape.
func TestMigrationWAL_PayloadBoundaries(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	require.NoError(t, w.Append(1, nil), "empty payload must be allowed")
	require.NoError(t, w.Append(2, []byte("non-empty")))
	payload3 := make([]byte, 64*1024)
	for i := range payload3 {
		payload3[i] = byte(i % 251)
	}
	require.NoError(t, w.Append(3, payload3))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	requireLatest(t, w2, 3, payload3)
}

// TestMigrationWAL_OpenRemovesOrphanTempFile simulates a crash between
// writing the .tmp file and the rename-to-final step. The .tmp must be
// discarded; the previously-durable record must survive.
func TestMigrationWAL_OpenRemovesOrphanTempFile(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	require.NoError(t, w.Append(1, []byte("committed")))

	// Plant a half-written tmp file as if Append(2) was in progress when the
	// process crashed.
	tmpPath := filepath.Join(dir, formatRecName(2)+walTempSuffix)
	require.NoError(t, os.WriteFile(tmpPath, []byte("partial"), 0o644))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	requireLatest(t, w2, 1, []byte("committed"))

	_, err = os.Stat(tmpPath)
	require.True(t, os.IsNotExist(err), "Open must have removed the orphan .tmp file")

	// Subsequent appends must continue from the previously durable batchID.
	require.NoError(t, w2.Append(2, []byte("retried")))
	requireLatest(t, w2, 2, []byte("retried"))
}

// TestMigrationWAL_OpenRemovesStaleRecords covers the case where a crash
// landed between the fsync of a newly-renamed record and the opportunistic
// deletion of its predecessor. Open must keep only the latest.
func TestMigrationWAL_OpenRemovesStaleRecords(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	require.NoError(t, w.Append(1, []byte("one")))

	// Plant a later durable record as if Append(2) succeeded but crashed
	// before unlinking 1.rec.
	require.NoError(t, writeRecordFile(filepath.Join(dir, formatRecName(2)+walTempSuffix), []byte("two")))
	require.NoError(t, os.Rename(
		filepath.Join(dir, formatRecName(2)+walTempSuffix),
		filepath.Join(dir, formatRecName(2)),
	))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	requireLatest(t, w2, 2, []byte("two"))

	_, err = os.Stat(filepath.Join(dir, formatRecName(1)))
	require.True(t, os.IsNotExist(err), "stale older record must have been removed")
}

// TestMigrationWAL_CleansUpPriorRecordAfterAppend confirms that the
// opportunistic cleanup in Append removes the old file without waiting for a
// reopen.
func TestMigrationWAL_CleansUpPriorRecordAfterAppend(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	require.NoError(t, w.Append(1, []byte("one")))
	require.FileExists(t, filepath.Join(dir, formatRecName(1)))

	require.NoError(t, w.Append(2, []byte("two")))

	_, err = os.Stat(filepath.Join(dir, formatRecName(1)))
	require.True(t, os.IsNotExist(err), "prior record must be cleaned up after a successful Append")
	require.FileExists(t, filepath.Join(dir, formatRecName(2)))
}

// TestMigrationWAL_ChecksumFailureIsHardError confirms that corruption of
// an on-disk record is reported to the caller rather than silently skipped.
func TestMigrationWAL_ChecksumFailureIsHardError(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	require.NoError(t, w.Append(1, []byte("victim")))

	// Flip a bit in the payload. The checksum (first 4 bytes) is left alone,
	// so the recorded checksum no longer matches.
	path := filepath.Join(dir, formatRecName(1))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o644))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	_, _, err = w2.Latest()
	require.Error(t, err, "Latest must surface checksum mismatches")
	require.Contains(t, err.Error(), "checksum")
}

// TestMigrationWAL_TruncatedRecordIsHardError covers a record file that is
// shorter than the checksum header - e.g., because someone truncated it.
func TestMigrationWAL_TruncatedRecordIsHardError(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	require.NoError(t, w.Append(1, []byte("x")))

	path := filepath.Join(dir, formatRecName(1))
	require.NoError(t, os.Truncate(path, 2))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	_, _, err = w2.Latest()
	require.Error(t, err)
	require.Contains(t, err.Error(), "truncated")
}

// TestMigrationWAL_UnknownFilesIgnored confirms that files we don't
// recognize (e.g., a stray .md file someone left behind) are left alone and
// don't upset Open or Latest.
func TestMigrationWAL_UnknownFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	require.NoError(t, w.Append(1, []byte("x")))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "garbage"), []byte("hi"), 0o644))

	w2, err := OpenMigrationWAL(dir)
	require.NoError(t, err)

	requireLatest(t, w2, 1, []byte("x"))
	require.FileExists(t, filepath.Join(dir, "README.md"))
	require.FileExists(t, filepath.Join(dir, "garbage"))
}

// TestMigrationWAL_OnDiskFormat confirms the literal on-disk layout the rest
// of the tests depend on, so that a future refactor cannot silently change
// the wire format.
func TestMigrationWAL_OnDiskFormat(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenMigrationWAL(dir)
	require.NoError(t, err)
	require.NoError(t, w.Append(1, []byte("hello")))

	data, err := os.ReadFile(filepath.Join(dir, formatRecName(1)))
	require.NoError(t, err)
	require.Equal(t, walRecHeaderSize+len("hello"), len(data))

	checksum := binary.BigEndian.Uint32(data[:walRecHeaderSize])
	require.Equal(t, crc32.ChecksumIEEE([]byte("hello")), checksum)
	require.Equal(t, []byte("hello"), data[walRecHeaderSize:])
}
