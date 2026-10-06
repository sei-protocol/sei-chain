package memiavl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMultiTreeImporterValidatesModuleName checks that AddModule only accepts
// plain directory names.
func TestMultiTreeImporterValidatesModuleName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "db")
	require.NoError(t, os.Mkdir(dir, 0o750))

	mti, err := NewMultiTreeImporter(dir, 1)
	require.NoError(t, err)

	for _, name := range []string{"", ".", "..", "../../other", "a/b", "/abs", filepath.Join(root, "abs")} {
		require.ErrorContains(t, mti.AddModule(name), "invalid snapshot module name", "name %q", name)
	}
	require.Nil(t, mti.importer, "a rejected name must not start a tree import")

	require.NoError(t, mti.AddModule("bank"))
	require.NoError(t, mti.Close())

	_, err = os.Stat(filepath.Join(root, "other"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(dir, snapshotName(1), "bank"))
	require.NoError(t, err)
}
