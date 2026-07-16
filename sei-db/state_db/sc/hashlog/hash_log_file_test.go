package hashlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsHashLogFileName(t *testing.T) {
	cases := []struct {
		name     string
		isHL     bool
		isSealed bool
	}{
		{"5-v1.0.0.hlog.u", true, false},
		{"5-100-200-v1.0.0.hlog", true, true},
		{"0-0-0-v1.2.3_rc1.hlog", true, true},
		{"notafile.txt", false, false},
		{"5.hlog.u", false, false},          // missing version
		{"5-100-v1.0.0.hlog", false, false}, // sealed needs two block numbers
		{"abc-v1.0.0.hlog.u", false, false}, // non-numeric index
	}
	for _, c := range cases {
		isHL, isSealed := isHashLogFileName(c.name)
		require.Equal(t, c.isHL, isHL, c.name)
		if isHL {
			require.Equal(t, c.isSealed, isSealed, c.name)
		}
	}
}

func TestParseBlockNumbersFromFileName(t *testing.T) {
	first, last, version, err := parseBlockNumbersFromFileName("3-10-20-v1.0.0.hlog")
	require.NoError(t, err)
	require.Equal(t, uint64(10), first)
	require.Equal(t, uint64(20), last)
	require.Equal(t, "v1.0.0", version)

	// Unsealed names carry only the index and version.
	first, last, version, err = parseBlockNumbersFromFileName("3-v1.0.0.hlog.u")
	require.NoError(t, err)
	require.Equal(t, uint64(0), first)
	require.Equal(t, uint64(0), last)
	require.Equal(t, "v1.0.0", version)

	_, _, _, err = parseBlockNumbersFromFileName("garbage")
	require.Error(t, err)
}

func newTestLog(blockNumber uint64, hashTypes []string) *HashLog {
	hashes := make(map[string][]byte, len(hashTypes))
	for _, hashType := range hashTypes {
		hashes[hashType] = []byte{byte(blockNumber), byte(len(hashType))}
	}
	return &HashLog{BlockNumber: blockNumber, Hashes: hashes}
}

func TestHashLogFileWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	hashTypes := []string{"changeset", "root"}

	file, err := newHashLogFile(dir, 1, "v1.0.0", hashTypes)
	require.NoError(t, err)
	require.NoError(t, file.write(newTestLog(10, hashTypes)))
	require.NoError(t, file.write(newTestLog(11, hashTypes)))
	require.NoError(t, file.close())

	sealedPath := filepath.Join(dir, "1-10-11-v1.0.0.hlog")
	require.FileExists(t, sealedPath)

	read, err := ReadHashLogFile(sealedPath)
	require.NoError(t, err)
	require.Equal(t, hashTypes, read.hashTypes)
	require.Len(t, read.logs, 2)
	require.Equal(t, uint64(10), read.logs[0].BlockNumber)
	require.Equal(t, "v1.0.0", read.logs[0].Version)
	require.Equal(t, []byte{10, 9}, read.logs[0].Hashes["changeset"])
	require.Equal(t, uint64(11), read.logs[1].BlockNumber)
}

func TestHashLogFileWriteRejectedAfterSeal(t *testing.T) {
	dir := t.TempDir()
	hashTypes := []string{"changeset"}
	file, err := newHashLogFile(dir, 1, "v1.0.0", hashTypes)
	require.NoError(t, err)
	require.NoError(t, file.write(newTestLog(1, hashTypes)))
	require.NoError(t, file.close())
	require.Error(t, file.write(newTestLog(2, hashTypes)))
}

func TestHashLogFileEmptyRemovedOnClose(t *testing.T) {
	dir := t.TempDir()
	file, err := newHashLogFile(dir, 1, "v1.0.0", []string{"changeset"})
	require.NoError(t, err)
	require.NoError(t, file.close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "an empty file should be removed, not sealed")
}

func TestReadHashLogFileTolaratesTornFinalLine(t *testing.T) {
	dir := t.TempDir()
	hashTypes := []string{"changeset", "root"}

	file, err := newHashLogFile(dir, 1, "v1.0.0", hashTypes)
	require.NoError(t, err)
	require.NoError(t, file.write(newTestLog(10, hashTypes)))
	require.NoError(t, file.write(newTestLog(11, hashTypes)))
	require.NoError(t, file.close())
	sealedPath := filepath.Join(dir, "1-10-11-v1.0.0.hlog")

	// Simulate a torn write: append a partial record with no trailing newline.
	f, err := os.OpenFile(sealedPath, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("12,deadbeef")
	require.NoError(t, f.Close())
	require.NoError(t, err)

	read, err := ReadHashLogFile(sealedPath)
	require.NoError(t, err)
	require.Len(t, read.logs, 2, "the torn final record must be discarded")
	require.Equal(t, uint64(11), read.logs[1].BlockNumber)
}

func TestSealHashLogRecoversOrphan(t *testing.T) {
	dir := t.TempDir()
	hashTypes := []string{"changeset"}

	// Create an unsealed file but do not seal it (simulating a crash).
	file, err := newHashLogFile(dir, 4, "v9.9.9", hashTypes)
	require.NoError(t, err)
	require.NoError(t, file.write(newTestLog(30, hashTypes)))
	require.NoError(t, file.write(newTestLog(31, hashTypes)))
	require.NoError(t, file.writer.Flush())
	require.NoError(t, file.file.Sync())
	require.NoError(t, file.file.Close())

	orphanPath := filepath.Join(dir, "4-v9.9.9.hlog.u")
	require.FileExists(t, orphanPath)

	require.NoError(t, sealHashLog(orphanPath))

	require.NoFileExists(t, orphanPath)
	sealedPath := filepath.Join(dir, "4-30-31-v9.9.9.hlog")
	require.FileExists(t, sealedPath)

	read, err := ReadHashLogFile(sealedPath)
	require.NoError(t, err)
	require.Len(t, read.logs, 2)
	require.Equal(t, "v9.9.9", read.logs[0].Version)
}
