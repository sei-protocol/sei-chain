package clickhouse

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

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

func TestMigrationFilesRejectsMisorderedVersions(t *testing.T) {
	_, _, err := migrationFiles(fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("SELECT 1")},
		"migrations/001_b.sql":  {Data: []byte("SELECT 1")},
	})
	require.ErrorContains(t, err, "does not follow")
	_, _, err = migrationFiles(fstest.MapFS{"migrations/a.sql": {Data: []byte("SELECT 1")}})
	require.ErrorContains(t, err, "not named")
}

func TestCheckMigrations(t *testing.T) {
	require.NoError(t, CheckMigrations(fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("SELECT 1;")}}))
	require.ErrorContains(t, CheckMigrations(fstest.MapFS{"migrations/0001_a.sql": {Data: []byte("-- nothing\n")}}), "no statements")
	require.ErrorContains(t, CheckMigrations(fstest.MapFS{"migrations/a.sql": {Data: []byte("SELECT 1;")}}), "not named")
}

func TestMigrateRejectsUnsafeDatabase(t *testing.T) {
	require.ErrorContains(t, Migrate(t.Context(), Config{Database: "a; DROP TABLE x"}, fstest.MapFS{}), "not a plain identifier")
}

func TestMigrateAppliesEachOnce(t *testing.T) {
	cfg := testConfig(t)
	fsys := fstest.MapFS{"migrations/0001_create.sql": {Data: []byte(
		"CREATE TABLE IF NOT EXISTS t (x UInt8) ENGINE = MergeTree ORDER BY x;\n")}}
	require.NoError(t, Migrate(t.Context(), cfg, fsys))
	require.NoError(t, Migrate(t.Context(), cfg, fsys), "migrating again changes nothing")

	// Not safe to run twice, so a second run would fail.
	fsys["migrations/0002_add_column.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE t ADD COLUMN y UInt8;\n")}
	require.NoError(t, Migrate(t.Context(), cfg, fsys))
	require.NoError(t, Migrate(t.Context(), cfg, fsys), "applied migrations are skipped")

	client, err := Open(cfg, Options{})
	require.NoError(t, err)
	defer client.Close()
	require.Equal(t, uint64(2), count(t, client, "SELECT count() AS a FROM schema_migrations"))
	require.Equal(t, uint64(1), count(t, client,
		"SELECT count() AS a FROM system.columns WHERE database = currentDatabase() AND table = 't' AND name = 'y'"))
}
