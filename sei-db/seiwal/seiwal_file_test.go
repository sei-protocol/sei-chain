package seiwal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileNaming(t *testing.T) {
	require.Equal(t, "3.wal.u", unsealedFileName(3))
	require.Equal(t, "3-10-20.wal", sealedFileName(3, 10, 20))

	parsed, ok := parseFileName("3.wal.u")
	require.True(t, ok)
	require.Equal(t, parsedFileName{fileSeq: 3, sealed: false}, parsed)

	parsed, ok = parseFileName("3-10-20.wal")
	require.True(t, ok)
	require.Equal(t, parsedFileName{fileSeq: 3, firstIndex: 10, lastIndex: 20, sealed: true}, parsed)

	_, ok = parseFileName("not-a-wal-file.txt")
	require.False(t, ok)
}

// writeMutableFile creates a mutable file at sequence 0, applies fn to it, then flushes and closes the
// underlying handle without sealing, leaving an unsealed file on disk. It returns the file path.
func writeMutableFile(t *testing.T, dir string, fn func(f *walFile)) string {
	t.Helper()
	f, err := newWalFile(dir, 0)
	require.NoError(t, err)
	fn(f)
	require.NoError(t, f.flush(true))
	require.NoError(t, f.file.Close())
	return filepath.Join(dir, unsealedFileName(0))
}

// writeRecordTo frames and appends a record for the given index to f.
func writeRecordTo(t *testing.T, f *walFile, index uint64, payload []byte) {
	t.Helper()
	require.NoError(t, f.writeRecord(frameRecord(index, payload), index))
}

func TestReadWalFileCleanTail(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeRecordTo(t, f, 1, []byte("one"))
		writeRecordTo(t, f, 2, []byte("two"))
	})

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasRecords)
	require.Equal(t, uint64(1), contents.firstIndex)
	require.Equal(t, uint64(2), contents.lastIndex)
	require.Len(t, contents.records, 2)
}

func TestReadWalFileTornTrailingRecord(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeRecordTo(t, f, 1, []byte("one"))
		writeRecordTo(t, f, 2, []byte("two"))
		// A third record whose framing is truncated mid-payload, as a torn write would leave.
		frame := frameRecord(3, []byte("three"))
		require.NoError(t, f.flush(false))
		_, err := f.writer.Write(frame[:len(frame)-3])
		require.NoError(t, err)
	})

	contents, err := readWalFile(path)
	require.NoError(t, err)
	// The torn record 3 is dropped; the last intact record is 2.
	require.True(t, contents.hasRecords)
	require.Equal(t, uint64(2), contents.lastIndex)
	require.Len(t, contents.records, 2)
}

func TestReadWalFilePartialLengthPrefix(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeRecordTo(t, f, 1, []byte("one"))
	})

	// Append a lone 0x80 byte: an incomplete uvarint prefix, as a torn write would leave.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.Write([]byte{0x80})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasRecords)
	require.Equal(t, uint64(1), contents.lastIndex)
	require.Len(t, contents.records, 1)
}

func TestReadWalFileMidRecordTruncation(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeRecordTo(t, f, 1, []byte("one"))
		writeRecordTo(t, f, 2, []byte("two"))
	})

	info, err := os.Stat(path)
	require.NoError(t, err)
	// Lop a few bytes off the end, tearing record 2.
	require.NoError(t, os.Truncate(path, info.Size()-3))

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasRecords)
	require.Equal(t, uint64(1), contents.lastIndex)
	require.Len(t, contents.records, 1)
}

func TestReadWalFileChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeRecordTo(t, f, 1, []byte("one"))
		writeRecordTo(t, f, 2, []byte("two"))
	})

	// Flip the final byte (part of record 2's CRC), so that record fails its checksum.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	contents, err := readWalFile(path)
	require.NoError(t, err)
	// Record 1 survives; the corrupt record 2 is dropped.
	require.True(t, contents.hasRecords)
	require.Equal(t, uint64(1), contents.lastIndex)
	require.Len(t, contents.records, 1)
}

func TestReadWalFileBadMagic(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeRecordTo(t, f, 1, []byte("one"))
	})

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[0] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = readWalFile(path)
	require.Error(t, err)
}
