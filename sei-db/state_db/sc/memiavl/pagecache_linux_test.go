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
	fd := int(file.file.Fd())
	require.NoError(t, unix.Fadvise(fd, 0, 0, unix.FADV_RANDOM))
	require.NoError(t, unix.Fadvise(fd, 0, 0, unix.FADV_DONTNEED))
}
