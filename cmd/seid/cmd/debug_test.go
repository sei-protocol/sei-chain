package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenDBPathVariants(t *testing.T) {
	t.Run("without trailing separator", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db")
		db, err := OpenDB(dbPath)
		require.NoError(t, err)
		require.NotNil(t, db)
		require.NoError(t, db.Close())
		_, err = os.Stat(dbPath)
		require.NoError(t, err)
	})

	t.Run("with trailing separator", func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db") + string(filepath.Separator)
		db, err := OpenDB(dbPath)
		require.NoError(t, err)
		require.NotNil(t, db)
		require.NoError(t, db.Close())
		_, err = os.Stat(strings.TrimSuffix(dbPath, string(filepath.Separator)))
		require.NoError(t, err)
	})

	t.Run("windows path", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("windows-specific test")
		}
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "test.db") + `\`
		db, err := OpenDB(dbPath)
		require.NoError(t, err)
		require.NotNil(t, db)
		require.NoError(t, db.Close())
		_, err = os.Stat(strings.TrimSuffix(dbPath, `\`))
		require.NoError(t, err)
	})
}
