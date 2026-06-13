package coordinator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanClosedFilesSortsByStartBlock(t *testing.T) {
	dir := t.TempDir()
	for _, startBlock := range []uint64{1000, 0, 500} {
		writeReceiptFile(t, dir, startBlock, []uint64{startBlock + 1})
		writeLogFile(t, dir, startBlock)
	}

	reader, err := NewReaderWithMaxBlocksPerFile(dir, 500)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	closedFiles, err := scanClosedFiles(dir, reader)
	require.NoError(t, err)
	require.Len(t, closedFiles, 3)
	require.Equal(t, uint64(0), closedFiles[0].startBlock)
	require.Equal(t, uint64(500), closedFiles[1].startBlock)
	require.Equal(t, uint64(1000), closedFiles[2].startBlock)
}
