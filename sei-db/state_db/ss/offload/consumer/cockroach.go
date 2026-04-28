package consumer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// CockroachConfig configures the CockroachDB sink. DSN follows the standard
// libpq/pgx format (e.g. postgresql://user@host:26257/db?sslmode=verify-full).
type CockroachConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration

	// EnableLatest UPSERTs into state_latest on every block so "current
	// state" reads are a single PK lookup. Cheap; ~2x the write rate.
	EnableLatest bool
}

func (c *CockroachConfig) ApplyDefaults() {
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 8
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = c.MaxOpenConns
	}
	if c.ConnMaxLifetime == 0 {
		c.ConnMaxLifetime = 30 * time.Minute
	}
}

func (c *CockroachConfig) Validate() error {
	if strings.TrimSpace(c.DSN) == "" {
		return fmt.Errorf("cockroach dsn is required")
	}
	if c.MaxOpenConns < 0 {
		return fmt.Errorf("cockroach max open conns must be non-negative")
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("cockroach max idle conns must be non-negative")
	}
	return nil
}

type cockroachSink struct {
	db           *sql.DB
	enableLatest bool
}

var _ Sink = (*cockroachSink)(nil)

// NewCockroachSink opens a pooled connection to CockroachDB. The caller is
// responsible for applying schema.sql beforehand.
func NewCockroachSink(cfg CockroachConfig) (Sink, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open cockroach: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping cockroach: %w", err)
	}

	return &cockroachSink{db: db, enableLatest: cfg.EnableLatest}, nil
}

func (s *cockroachSink) Close() error {
	return s.db.Close()
}

func (s *cockroachSink) LastVersion(ctx context.Context) (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT max(version) FROM state_versions`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("read last version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Int64, nil
}

func (s *cockroachSink) Write(ctx context.Context, rec Record) error {
	if rec.Entry == nil {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertVersion(ctx, tx, rec); err != nil {
		return err
	}
	if err := insertMutations(ctx, tx, rec); err != nil {
		return err
	}
	if s.enableLatest {
		if err := upsertLatest(ctx, tx, rec); err != nil {
			return err
		}
	}
	if err := insertUpgrades(ctx, tx, rec); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func insertVersion(ctx context.Context, tx *sql.Tx, rec Record) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO state_versions (version, kafka_topic, kafka_offset)
		VALUES ($1, $2, $3)
		ON CONFLICT (version) DO NOTHING
	`, rec.Entry.Version, rec.Topic, rec.Offset)
	if err != nil {
		return fmt.Errorf("insert version: %w", err)
	}
	return nil
}

// mutationBatchRows caps rows per INSERT; CockroachDB handles large batches
// but smaller batches keep transaction retries cheap under contention.
const mutationBatchRows = 500

// mutationBatch is one ready-to-execute INSERT with its parameter list.
type mutationBatch struct {
	Stmt string
	Args []interface{}
}

// buildMutationBatches turns a ChangelogEntry into one or more parameterized
// INSERT statements. Pure function — no DB access — so it is unit-testable.
func buildMutationBatches(rec Record, maxRows int) []mutationBatch {
	if maxRows <= 0 {
		maxRows = mutationBatchRows
	}
	version := rec.Entry.Version
	const colsPerRow = 5
	var (
		batches []mutationBatch
		args    = make([]interface{}, 0, maxRows*colsPerRow)
		parts   = make([]string, 0, maxRows)
	)
	flush := func() {
		if len(parts) == 0 {
			return
		}
		stmt := `INSERT INTO state_mutations (store_name, key, version, value, deleted) VALUES ` +
			strings.Join(parts, ",") +
			` ON CONFLICT (store_name, key, version) DO UPDATE SET value = excluded.value, deleted = excluded.deleted`
		batches = append(batches, mutationBatch{Stmt: stmt, Args: args})
		args = make([]interface{}, 0, maxRows*colsPerRow)
		parts = make([]string, 0, maxRows)
	}

	for _, ncs := range rec.Entry.Changesets {
		for _, p := range ncs.Changeset.Pairs {
			idx := len(args)
			parts = append(parts, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d)", idx+1, idx+2, idx+3, idx+4, idx+5))
			args = append(args, ncs.Name, p.Key, version, p.Value, p.Delete)
			if len(parts) >= maxRows {
				flush()
			}
		}
	}
	flush()
	return batches
}

func insertMutations(ctx context.Context, tx *sql.Tx, rec Record) error {
	for _, b := range buildMutationBatches(rec, mutationBatchRows) {
		if _, err := tx.ExecContext(ctx, b.Stmt, b.Args...); err != nil {
			return fmt.Errorf("insert mutations: %w", err)
		}
	}
	return nil
}

// buildLatestBatches builds UPSERT INTO state_latest batches. The WHERE
// clause guards against out-of-order writes from parallel partition workers
// — a row is only overwritten if the incoming version is at least as new.
func buildLatestBatches(rec Record, maxRows int) []mutationBatch {
	if maxRows <= 0 {
		maxRows = mutationBatchRows
	}
	version := rec.Entry.Version
	const colsPerRow = 5
	var (
		batches []mutationBatch
		args    = make([]interface{}, 0, maxRows*colsPerRow)
		parts   = make([]string, 0, maxRows)
	)
	flush := func() {
		if len(parts) == 0 {
			return
		}
		stmt := `INSERT INTO state_latest (store_name, key, value, version, deleted) VALUES ` +
			strings.Join(parts, ",") +
			` ON CONFLICT (store_name, key) DO UPDATE
			    SET value = excluded.value, version = excluded.version, deleted = excluded.deleted
			    WHERE state_latest.version <= excluded.version`
		batches = append(batches, mutationBatch{Stmt: stmt, Args: args})
		args = make([]interface{}, 0, maxRows*colsPerRow)
		parts = make([]string, 0, maxRows)
	}
	for _, ncs := range rec.Entry.Changesets {
		for _, p := range ncs.Changeset.Pairs {
			idx := len(args)
			parts = append(parts, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d)", idx+1, idx+2, idx+3, idx+4, idx+5))
			args = append(args, ncs.Name, p.Key, p.Value, version, p.Delete)
			if len(parts) >= maxRows {
				flush()
			}
		}
	}
	flush()
	return batches
}

func upsertLatest(ctx context.Context, tx *sql.Tx, rec Record) error {
	for _, b := range buildLatestBatches(rec, mutationBatchRows) {
		if _, err := tx.ExecContext(ctx, b.Stmt, b.Args...); err != nil {
			return fmt.Errorf("upsert state_latest: %w", err)
		}
	}
	return nil
}

func insertUpgrades(ctx context.Context, tx *sql.Tx, rec Record) error {
	if len(rec.Entry.Upgrades) == 0 {
		return nil
	}
	for _, up := range rec.Entry.Upgrades {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO state_tree_upgrades (version, name, rename_from, delete)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (version, name) DO UPDATE
			    SET rename_from = excluded.rename_from, delete = excluded.delete
		`, rec.Entry.Version, up.Name, up.RenameFrom, up.Delete)
		if err != nil {
			return fmt.Errorf("insert upgrade: %w", err)
		}
	}
	return nil
}
