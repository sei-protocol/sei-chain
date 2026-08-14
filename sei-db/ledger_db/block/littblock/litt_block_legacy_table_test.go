package littblock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A home written before the table rename must not open as an empty store. The data under the old
// name is unreachable either way; the only question is whether the operator is told, and a healthy
// empty store tells them nothing until the history they wanted is already gone.
func TestOpenRefusesAPreRenameTable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, legacyTableName, "segments"), 0o700))

	_, err := NewBlockDB(gcConfig(t, dir))
	require.ErrorContains(t, err, legacyTableName)
	require.ErrorContains(t, err, "Delete or move the directory aside")
}

// The refusal happens before littdb builds anything, so a refused open leaves the directory as it
// found it. Otherwise the operator inspecting the failure would find a new empty table sitting
// next to the old one, and could not tell which of the two the error was about.
func TestRefusedOpenLeavesTheDirectoryUntouched(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, legacyTableName, "segments"), 0o700))

	_, err := NewBlockDB(gcConfig(t, dir))
	require.Error(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing may be created beside the legacy table")
	require.Equal(t, legacyTableName, entries[0].Name())
}

// The guard keys on the old name specifically: a normal home has no such directory and must open,
// and the check must not be satisfied by the store's own table.
func TestOpenAcceptsAHomeWithoutAPreRenameTable(t *testing.T) {
	dir := t.TempDir()

	db, err := NewBlockDB(gcConfig(t, dir))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	require.DirExists(t, filepath.Join(dir, tableName), "the store builds its table under the current name")

	db2, err := NewBlockDB(gcConfig(t, dir))
	require.NoError(t, err, "reopening a store it just wrote must not trip the guard")
	require.NoError(t, db2.Close())
}

// Every root is checked, not just the first: littdb spreads a table across all of them, so data
// under the old name in any one of them is data this store cannot reach.
func TestOpenRefusesAPreRenameTableInANonFirstRoot(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(second, legacyTableName), 0o700))

	require.NoError(t, refuseLegacyTable([]string{first}))
	require.ErrorContains(t, refuseLegacyTable([]string{first, second}), second)
}
