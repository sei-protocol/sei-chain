package statewal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestFileNaming(t *testing.T) {
	require.Equal(t, "3.swal.u", unsealedFileName(3))
	require.Equal(t, "3-10-20.swal", sealedFileName(3, 10, 20))

	parsed, ok := parseFileName("3.swal.u")
	require.True(t, ok)
	require.Equal(t, parsedFileName{index: 3, sealed: false}, parsed)

	parsed, ok = parseFileName("3-10-20.swal")
	require.True(t, ok)
	require.Equal(t, parsedFileName{index: 3, firstBlock: 10, lastBlock: 20, sealed: true}, parsed)

	_, ok = parseFileName("not-a-wal-file.txt")
	require.False(t, ok)
}

// writeMutableFile creates a mutable file at index 0, applies fn to it, then flushes and closes the underlying
// handle without sealing, leaving an unsealed file on disk. It returns the file path.
func writeMutableFile(t *testing.T, dir string, fn func(f *walFile)) string {
	t.Helper()
	f, err := newWalFile(dir, 0)
	require.NoError(t, err)
	fn(f)
	require.NoError(t, f.flush(true))
	require.NoError(t, f.file.Close())
	return filepath.Join(dir, unsealedFileName(0))
}

func writeCompleteBlock(t *testing.T, f *walFile, block uint64) {
	t.Helper()
	cs := []*proto.NamedChangeSet{makeChangeSet("evm", []byte{byte(block)}, []byte{byte(block)})}
	require.NoError(t, f.writeEntry(NewEntry(block, cs)))
	require.NoError(t, f.writeEntry(NewEndOfBlockEntry(block)))
}

func TestReadWalFileCleanTail(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeCompleteBlock(t, f, 1)
		writeCompleteBlock(t, f, 2)
	})

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasCompleteBlock)
	require.Equal(t, uint64(1), contents.firstBlock)
	require.Equal(t, uint64(2), contents.lastCompleteBlock)
	require.Len(t, contents.entries, 4) // 2 changeset + 2 end-of-block records
}

func TestReadWalFileIncompleteTailBlock(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeCompleteBlock(t, f, 1)
		writeCompleteBlock(t, f, 2)
		// Block 3 changeset with no end-of-block marker.
		require.NoError(t, f.writeEntry(NewEntry(3,
			[]*proto.NamedChangeSet{makeChangeSet("evm", []byte{3}, []byte{3})})))
	})

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasCompleteBlock)
	require.Equal(t, uint64(2), contents.lastCompleteBlock)
	// The dangling block-3 changeset is read as an entry, but the completed boundary stops at block 2.
	require.Equal(t, uint64(3), contents.lastBlock)
}

func TestReadWalFilePartialLengthPrefix(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeCompleteBlock(t, f, 1)
	})

	// Append a lone 0x80 byte: an incomplete uvarint length prefix, as a torn write would leave.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.Write([]byte{0x80})
	require.NoError(t, err)
	require.NoError(t, f.Close())

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasCompleteBlock)
	require.Equal(t, uint64(1), contents.lastCompleteBlock)
	require.Len(t, contents.entries, 2)
}

func TestReadWalFileMidRecordTruncation(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeCompleteBlock(t, f, 1)
		writeCompleteBlock(t, f, 2)
	})

	info, err := os.Stat(path)
	require.NoError(t, err)
	// Lop a few bytes off the end, tearing block 2's end-of-block record.
	require.NoError(t, os.Truncate(path, info.Size()-3))

	contents, err := readWalFile(path)
	require.NoError(t, err)
	require.True(t, contents.hasCompleteBlock)
	require.Equal(t, uint64(1), contents.lastCompleteBlock)
}

func TestReadWalFileChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeCompleteBlock(t, f, 1)
	})

	// Flip the final byte (part of the end-of-block record's CRC), so that record fails its checksum.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)-1] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	contents, err := readWalFile(path)
	require.NoError(t, err)
	// The changeset record survives; the corrupt end-of-block record is dropped, so no complete block remains.
	require.False(t, contents.hasCompleteBlock)
	require.Len(t, contents.entries, 1)
}

func TestReadWalFileBadMagic(t *testing.T) {
	dir := t.TempDir()
	path := writeMutableFile(t, dir, func(f *walFile) {
		writeCompleteBlock(t, f, 1)
	})

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[0] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0o600))

	_, err = readWalFile(path)
	require.Error(t, err)
}
