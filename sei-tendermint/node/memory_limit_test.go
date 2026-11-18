package node

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestCgroupMemoryLimitTakesTheTightestAncestor(t *testing.T) {
	cgroupFS := fstest.MapFS{
		"memory.max":                           {Data: []byte("max\n")},
		"system.slice/memory.max":              {Data: []byte("8589934592\n")},
		"system.slice/seid.service/memory.max": {Data: []byte("max\n")},
	}
	require.Equal(t, int64(8589934592), cgroupMemoryLimit(cgroupFS, "0::/system.slice/seid.service\n"))
}

func TestCgroupMemoryLimitReadsCgroupV1(t *testing.T) {
	cgroupFS := fstest.MapFS{
		"memory/memory.limit_in_bytes":          {Data: []byte("9223372036854771712\n")},
		"memory/kubepods/memory.limit_in_bytes": {Data: []byte("4294967296\n")},
	}
	self := "12:cpu,cpuacct:/kubepods\n11:memory:/kubepods\n"
	require.Equal(t, int64(4294967296), cgroupMemoryLimit(cgroupFS, self))
}

func TestCgroupMemoryLimitIsZeroWithoutALimit(t *testing.T) {
	cgroupFS := fstest.MapFS{
		"memory.max":                   {Data: []byte("max\n")},
		"pod/memory.max":               {Data: []byte("\n")},
		"memory/memory.limit_in_bytes": {Data: []byte("9223372036854771712\n")},
	}
	require.Zero(t, cgroupMemoryLimit(cgroupFS, "0::/pod\n"))
	require.Zero(t, cgroupMemoryLimit(cgroupFS, "5:memory:/\n"))
	require.Zero(t, cgroupMemoryLimit(cgroupFS, ""))
}
