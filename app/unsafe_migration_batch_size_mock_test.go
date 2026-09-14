//go:build mock_block_validation

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadUnsafeMigrationBatchSize(t *testing.T) {
	v, ok := readUnsafeMigrationBatchSize("")
	require.False(t, ok)
	require.Zero(t, v)

	v, ok = readUnsafeMigrationBatchSize("250000")
	require.True(t, ok)
	require.Equal(t, uint64(250000), v)

	v, ok = readUnsafeMigrationBatchSize("0")
	require.True(t, ok)
	require.Zero(t, v)

	require.Panics(t, func() { readUnsafeMigrationBatchSize("lots") })
	require.Panics(t, func() { readUnsafeMigrationBatchSize("-1") })
}

func TestUnsafeMigrationBatchSizeOverride_FollowsProcessEnv(t *testing.T) {
	// The override is fixed at process start, so assert against whatever this
	// test process was started with rather than mutating the environment.
	got := unsafeMigrationBatchSizeOverride(7)
	if unsafeMigrationBatchSizeSet {
		require.Equal(t, unsafeMigrationBatchSize, got)
		return
	}
	require.Equal(t, uint64(7), got)
}
