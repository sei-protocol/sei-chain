package memiavl

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// evictFromPageCache drops the pages of file from the page cache and turns off the kernel's read-ahead
// of reads from it, so the pages a test finds resident afterwards are the ones read.
func evictFromPageCache(t *testing.T, file *MmapFile) {
	t.Helper()
	_, err := unix.FcntlInt(file.file.Fd(), unix.F_RDAHEAD, 0)
	require.NoError(t, err)
	if len(file.data) > 0 {
		require.NoError(t, unix.Msync(file.data, unix.MS_INVALIDATE))
	}
}
