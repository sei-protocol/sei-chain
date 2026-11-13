package archive

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/ClickHouse/ch-go"
	"github.com/stretchr/testify/require"
)

func TestMigrationsAreNumberedInOrder(t *testing.T) {
	files, versions, err := migrationFiles(migrations)
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for i, file := range files {
		require.Equal(t, uint32(i+1), versions[i], file)
		sql, err := fs.ReadFile(migrations, file)
		require.NoError(t, err)
		require.NotEmpty(t, statements(string(sql)), file)
	}
}

func TestStatements(t *testing.T) {
	require.Equal(t, []string{"CREATE TABLE a (x UInt8)", "ALTER TABLE a\n  ADD COLUMN y UInt8"}, statements(`
-- a comment;
CREATE TABLE a (x UInt8);

ALTER TABLE a
  -- adds y
  ADD COLUMN y UInt8;
-- trailing comment
`))
}

func TestMigrationVersion(t *testing.T) {
	version, err := migrationVersion("0012_add_column.sql")
	require.NoError(t, err)
	require.Equal(t, uint32(12), version)
	for _, name := range []string{"add_column.sql", "0000_zero.sql", "x1_a.sql", "0001.sql"} {
		_, err := migrationVersion(name)
		require.Error(t, err, name)
	}
}

func TestApplyMigrationsRunsEachOnce(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	require.NoError(t, migrate(t.Context(), cfg), "migrating again changes nothing")
	require.Equal(t, []uint64{1}, query(t, cfg, "SELECT toUInt64(version) AS a FROM schema_migrations"))

	client, err := ch.Dial(t.Context(), cfg.options(cfg.Database))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	initial, err := fs.ReadFile(migrations, "migrations/0001_create_tables.sql")
	require.NoError(t, err)
	fsys := fstest.MapFS{
		"migrations/0001_create_tables.sql": {Data: initial},
		// Not safe to run twice, so a second run would fail.
		"migrations/0002_add_column.sql": {Data: []byte("ALTER TABLE blocks ADD COLUMN extra UInt8;\n")},
	}
	require.NoError(t, applyMigrations(t.Context(), client, fsys))
	require.NoError(t, applyMigrations(t.Context(), client, fsys), "applied migrations are skipped")
	require.Equal(t, []uint64{1, 2}, query(t, cfg, "SELECT toUInt64(version) AS a FROM schema_migrations ORDER BY a"))
	require.Equal(t, []uint64{1}, query(t, cfg,
		"SELECT count() AS a FROM system.columns WHERE database = currentDatabase() AND table = 'blocks' AND name = 'extra'"))
}

func TestMigrationFilesRejectsMisorderedVersions(t *testing.T) {
	_, _, err := migrationFiles(fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("SELECT 1")},
		"migrations/001_b.sql":  {Data: []byte("SELECT 1")},
	})
	require.ErrorContains(t, err, "does not follow")
	_, _, err = migrationFiles(fstest.MapFS{"migrations/a.sql": {Data: []byte("SELECT 1")}})
	require.ErrorContains(t, err, "not named")
}
