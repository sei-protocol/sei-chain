package consumer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// CockroachConfig configures the CockroachDB sink. DSN follows the standard
// libpq/pgx format (e.g. postgresql://user@host:26257/db?sslmode=verify-full).
type CockroachConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func (c *CockroachConfig) ApplyDefaults() {
	if c.MaxOpenConns == 0 {
		// Sized for parallel partition workers + COPY headroom; raise on
		// large clusters with many partitions.
		c.MaxOpenConns = 32
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
	db *sql.DB
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

	return &cockroachSink{db: db}, nil
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

	// Insert the version row first; if it already exists this is a Kafka
	// redelivery and the rest of the work is already on disk. Skip and commit
	// an empty tx so the offset advances.
	fresh, err := insertVersion(ctx, tx, rec)
	if err != nil {
		return err
	}
	if !fresh {
		return tx.Commit()
	}

	if err := copyMutations(ctx, tx, rec); err != nil {
		return err
	}
	if err := insertUpgrades(ctx, tx, rec); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// insertVersion returns true when the row was inserted (fresh write). False
// means another worker — or this same worker on a prior delivery — already
// committed this version.
func insertVersion(ctx context.Context, tx *sql.Tx, rec Record) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO state_versions (version, kafka_topic, kafka_offset)
		VALUES ($1, $2, $3)
		ON CONFLICT (version) DO NOTHING
	`, rec.Entry.Version, rec.Topic, rec.Offset)
	if err != nil {
		return false, fmt.Errorf("insert version: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("version rows affected: %w", err)
	}
	return n > 0, nil
}

// copyMutations streams rows into state_mutations via the COPY protocol.
// COPY is several times faster than multi-row INSERT for bulk loads. Since
// this path runs only for fresh versions (gated by insertVersion), there's
// no PK conflict to worry about — retries take the no-op skip path instead.
func copyMutations(ctx context.Context, tx *sql.Tx, rec Record) error {
	total := 0
	for _, ncs := range rec.Entry.Changesets {
		total += len(ncs.Changeset.Pairs)
	}
	if total == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, pq.CopyIn("state_mutations",
		"store_name", "key", "version", "value", "deleted"))
	if err != nil {
		return fmt.Errorf("prepare copy: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	version := rec.Entry.Version
	for _, ncs := range rec.Entry.Changesets {
		for _, p := range ncs.Changeset.Pairs {
			if _, err := stmt.ExecContext(ctx, ncs.Name, p.Key, version, p.Value, p.Delete); err != nil {
				return fmt.Errorf("copy mutation row: %w", err)
			}
		}
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("flush copy: %w", err)
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
