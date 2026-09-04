package evm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeMarkedDir(t *testing.T, path, marker string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "marker"), []byte(marker), 0o600))
}

func markerOf(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(path, "marker"))
	require.NoError(t, err)
	return string(got)
}

// requireNoLeftovers asserts nothing a restore stages beside dst outlived the heal.
func requireNoLeftovers(t *testing.T, dst string) {
	t.Helper()
	for _, leftover := range []string{dst + restoreTmpSuffix, dst + restoreBakSuffix} {
		_, err := os.Stat(leftover)
		require.True(t, os.IsNotExist(err), "%q is a full copy of the store and must not survive", leftover)
	}
}

// A restore interrupted between the two renames that swap the new copy in leaves no directory at all,
// which is otherwise indistinguishable from a store that was never written: the open creates an empty
// one, the head reads 0, and a catch-up stamps its target over almost no state.
func TestHealInterruptedRestore(t *testing.T) {
	t.Run("promotes the staged copy over the displaced one", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst+restoreTmpSuffix, "staged")
		writeMarkedDir(t, dst+restoreBakSuffix, "displaced")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "staged", markerOf(t, dst),
			"landing on the snapshot is what the interrupted rewind was for")
		requireNoLeftovers(t, dst)
	})

	t.Run("falls back to the displaced copy", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst+restoreBakSuffix, "displaced")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "displaced", markerOf(t, dst))
	})

	t.Run("leaves a store that has never been restored absent", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")

		require.NoError(t, healInterruptedRestore(dst))

		_, err := os.Stat(dst)
		require.True(t, os.IsNotExist(err), "with nothing to promote the directory must not appear")
	})

	t.Run("leaves a store that is already there alone", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst, "live")
		writeMarkedDir(t, dst+restoreTmpSuffix, "staged")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "live", markerOf(t, dst))
		requireNoLeftovers(t, dst)
	})

	// Only a later restore of this same directory would clear a leftover, so a node that crashed once
	// and never rewinds again carries a second copy of the store for good.
	t.Run("clears a leftover the swap never consumed", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "db")
		writeMarkedDir(t, dst, "live")
		writeMarkedDir(t, dst+restoreBakSuffix, "displaced")

		require.NoError(t, healInterruptedRestore(dst))

		require.Equal(t, "live", markerOf(t, dst))
		requireNoLeftovers(t, dst)
	})
}
