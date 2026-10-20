package hashlog

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// openTestWriter opens a HashLogWriter on dir.
func openTestWriter(t *testing.T, dir string) *HashLogWriter {
	t.Helper()
	w, err := NewHashLogWriter(testConfig(dir))
	require.NoError(t, err)
	return w
}

// readFileHeaders returns the column set of each hash log file in dir, in file order.
func readFileHeaders(t *testing.T, dir string) [][]string {
	t.Helper()
	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	headers := make([][]string, 0, len(files))
	for _, f := range files {
		file, err := ReadHashLogFile(filepath.Join(dir, f.name))
		require.NoError(t, err)
		headers = append(headers, file.hashTypes)
	}
	return headers
}

func TestWriterRotatesWhenNamesChange(t *testing.T) {
	dir := t.TempDir()
	w := openTestWriter(t, dir)
	require.NoError(t, w.Write(1, map[string][]byte{"b": {0x11}, "a": {0x01}}))
	require.NoError(t, w.Write(2, map[string][]byte{"a": {0x02}, "b": {0x12}}))
	require.NoError(t, w.Write(3, map[string][]byte{"c": {0x23}, "b": {0x13}}))
	require.NoError(t, w.Close())

	require.Equal(t, [][]string{{"a", "b"}, {"b", "c"}}, readFileHeaders(t, dir))
	logs := readAllLogs(t, dir)
	require.Len(t, logs, 3)
	require.Equal(t, map[string][]byte{"a": {0x01}, "b": {0x11}}, logs[0].Hashes)
	require.Equal(t, map[string][]byte{"a": {0x02}, "b": {0x12}}, logs[1].Hashes)
	require.Equal(t, map[string][]byte{"b": {0x13}, "c": {0x23}}, logs[2].Hashes)
}

func TestWriterDropsIllegalNames(t *testing.T) {
	dir := t.TempDir()
	w := openTestWriter(t, dir)
	require.NoError(t, w.Write(1, map[string][]byte{"a,b": {0x11}, "a": {0x01}}))

	// The writer keeps running.
	require.NoError(t, w.Write(2, map[string][]byte{"a": {0x02}}))
	require.NoError(t, w.Close())

	require.Equal(t, [][]string{{"a"}}, readFileHeaders(t, dir))
	logs := readAllLogs(t, dir)
	require.Len(t, logs, 2)
	require.Equal(t, map[string][]byte{"a": {0x01}}, logs[0].Hashes)
	require.Equal(t, map[string][]byte{"a": {0x02}}, logs[1].Hashes)
}

func TestWriterWritesABlockWithNoHashes(t *testing.T) {
	dir := t.TempDir()
	w := openTestWriter(t, dir)
	require.NoError(t, w.Write(1, map[string][]byte{}))
	require.NoError(t, w.Close())

	logs := readAllLogs(t, dir)
	require.Len(t, logs, 1)
	require.Equal(t, uint64(1), logs[0].BlockNumber)
	require.Empty(t, logs[0].Hashes)
}

func TestWriterFlushMakesRowsReadableBeforeClose(t *testing.T) {
	dir := t.TempDir()
	w := openTestWriter(t, dir)
	for block := uint64(1); block <= 3; block++ {
		require.NoError(t, w.Write(block, map[string][]byte{"a": {byte(block)}}))
	}
	require.NoError(t, w.Flush())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.False(t, files[0].parsed.sealed)
	require.Len(t, readAllLogs(t, dir), 3)
	require.NoError(t, w.Close())
}

func TestWriterResumesAfterReopen(t *testing.T) {
	dir := t.TempDir()
	w := openTestWriter(t, dir)
	require.NoError(t, w.Write(1, map[string][]byte{"a": {0x01}}))
	require.NoError(t, w.Close())

	reopened := openTestWriter(t, dir)
	require.NoError(t, reopened.Write(2, map[string][]byte{"a": {0x02}}))
	require.NoError(t, reopened.Close())

	files, err := listArchiveFiles(dir)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, uint64(0), files[0].parsed.index)
	require.Equal(t, uint64(1), files[1].parsed.index)
	logs, err := ReadHashForBlock(dir, 2)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, []byte{0x02}, logs[0].Hashes["a"])
}

func TestWriterCallsAfterCloseFail(t *testing.T) {
	w := openTestWriter(t, t.TempDir())
	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
	require.Error(t, w.Write(1, map[string][]byte{"a": {0x01}}))
	require.Error(t, w.Flush())
}

func TestWriterRejectsInvalidConfig(t *testing.T) {
	config := testConfig(t.TempDir())
	config.TargetFileSize = 0
	_, err := NewHashLogWriter(config)
	require.Error(t, err)
}

func TestFlatKVComponentHashesNamesEachDatabaseAndModule(t *testing.T) {
	hash := flatKVBlockHash(t, 1)
	hash.PerModule = map[string]map[string]*lthash.LtHash{
		"storage": {"evm": distinctLtHash(t, 0x20), "bank": distinctLtHash(t, 0x21)},
	}

	require.Equal(t, map[string][]byte{
		FlatKVDBHashPrefix + "account":          checksumOf(hash.PerDB["account"]),
		FlatKVDBHashPrefix + "code":             checksumOf(hash.PerDB["code"]),
		FlatKVDBHashPrefix + "storage":          checksumOf(hash.PerDB["storage"]),
		FlatKVDBHashPrefix + "misc":             checksumOf(hash.PerDB["misc"]),
		FlatKVModuleHashPrefix + "storage/evm":  checksumOf(hash.PerModule["storage"]["evm"]),
		FlatKVModuleHashPrefix + "storage/bank": checksumOf(hash.PerModule["storage"]["bank"]),
	}, FlatKVComponentHashes(hash))
}
