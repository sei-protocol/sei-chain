package archive

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
)

// migrations holds the archive's schema migrations, migrations/NNNN_name.sql, applied in version
// order. A migration is ClickHouse statements separated by ';', and must be safe to run again:
// ClickHouse DDL is not transactional, so a migration that fails part way leaves its earlier
// statements applied and is retried whole.
//
//go:embed migrations/*.sql
var migrations embed.FS

const createMigrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations
(
    version     UInt32,
    name        String,
    applied_at  DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree
ORDER BY version`

// migrate creates the archive database when it is missing and applies the migrations it has not
// applied yet.
func migrate(ctx context.Context, cfg Config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := run(ctx, cfg.options("default"), []string{"CREATE DATABASE IF NOT EXISTS " + cfg.Database}); err != nil {
		return err
	}
	client, err := ch.Dial(ctx, cfg.options(cfg.Database))
	if err != nil {
		return fmt.Errorf("dial clickhouse %s: %w", cfg.Addr, err)
	}
	defer func() { _ = client.Close() }()
	return applyMigrations(ctx, client, migrations)
}

// applyMigrations applies, in version order, the migrations in fsys that schema_migrations does
// not record, and records each once all its statements have succeeded.
func applyMigrations(ctx context.Context, client *ch.Client, fsys fs.FS) error {
	if err := client.Do(ctx, ch.Query{Body: createMigrationsTable}); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied, err := appliedVersions(ctx, client)
	if err != nil {
		return err
	}
	files, versions, err := migrationFiles(fsys)
	if err != nil {
		return err
	}
	for i, file := range files {
		name, version := path.Base(file), versions[i]
		if applied[version] {
			continue
		}
		sql, err := fs.ReadFile(fsys, file)
		if err != nil {
			return err
		}
		for _, stmt := range statements(string(sql)) {
			if err := client.Do(ctx, ch.Query{Body: stmt}); err != nil {
				return fmt.Errorf("migration %s: apply %q: %w", name, firstLine(stmt), err)
			}
		}
		var versionCol proto.ColUInt32
		var nameCol proto.ColStr
		versionCol.Append(version)
		nameCol.Append(name)
		record := proto.Input{{Name: "version", Data: &versionCol}, {Name: "name", Data: &nameCol}}
		if err := client.Do(ctx, ch.Query{Body: record.Into("schema_migrations"), Input: record}); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		logger.Info("archive migration applied", "migration", name)
	}
	return nil
}

func appliedVersions(ctx context.Context, client *ch.Client) (map[uint32]bool, error) {
	var versions proto.ColUInt32
	applied := map[uint32]bool{}
	err := client.Do(ctx, ch.Query{
		Body:   "SELECT version FROM schema_migrations",
		Result: proto.Results{{Name: "version", Data: &versions}},
		OnResult: func(context.Context, proto.Block) error {
			for _, v := range versions {
				applied[v] = true
			}
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	return applied, nil
}

// migrationFiles returns fsys's migration files and their versions, in version order. It fails
// on a misnamed file or a version that does not follow the one before it.
func migrationFiles(fsys fs.FS) ([]string, []uint32, error) {
	files, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, nil, err
	}
	versions := make([]uint32, len(files))
	for i, file := range files {
		if versions[i], err = migrationVersion(path.Base(file)); err != nil {
			return nil, nil, err
		}
		if i > 0 && versions[i] <= versions[i-1] {
			return nil, nil, fmt.Errorf("migration %s: version %d does not follow %d", path.Base(file), versions[i], versions[i-1])
		}
	}
	return files, versions, nil
}

// migrationVersion parses the version of a migration named NNNN_name.sql.
func migrationVersion(name string) (uint32, error) {
	prefix, _, ok := strings.Cut(name, "_")
	version, err := strconv.ParseUint(prefix, 10, 32)
	if !ok || err != nil || version == 0 {
		return 0, fmt.Errorf("migration %s is not named NNNN_name.sql", name)
	}
	return uint32(version), nil
}

// statements splits a migration into its statements, dropping '--' comment lines.
func statements(sql string) []string {
	var body strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			body.WriteString(line)
		}
	}
	var out []string
	for stmt := range strings.SplitSeq(body.String(), ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

// run executes stmts in order on one connection.
func run(ctx context.Context, opts ch.Options, stmts []string) error {
	client, err := ch.Dial(ctx, opts)
	if err != nil {
		return fmt.Errorf("dial clickhouse %s: %w", opts.Address, err)
	}
	defer func() { _ = client.Close() }()
	for _, stmt := range stmts {
		if err := client.Do(ctx, ch.Query{Body: stmt}); err != nil {
			return fmt.Errorf("apply %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
