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
var _ BatchSink = (*cockroachSink)(nil)

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
	return s.WriteBatch(ctx, []Record{rec})
}

func (s *cockroachSink) WriteBatch(ctx context.Context, records []Record) error {
	records = compactRecords(records)
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	fresh, err := insertVersions(ctx, tx, records)
	if err != nil {
		return err
	}
	if len(fresh) == 0 {
		return tx.Commit()
	}

	freshRecords := make([]Record, 0, len(fresh))
	for _, rec := range records {
		if _, ok := fresh[rec.Entry.Version]; ok {
			freshRecords = append(freshRecords, rec)
		}
	}

	if err := copyMutations(ctx, tx, freshRecords); err != nil {
		return err
	}
	if err := insertUpgrades(ctx, tx, freshRecords); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func compactRecords(records []Record) []Record {
	out := make([]Record, 0, len(records))
	for _, rec := range records {
		if rec.Entry != nil {
			out = append(out, rec)
		}
	}
	return out
}

func insertVersions(ctx context.Context, tx *sql.Tx, records []Record) (map[int64]struct{}, error) {
	var b strings.Builder
	args := make([]interface{}, 0, len(records)*4)
	b.WriteString(`
		INSERT INTO state_versions (version, kafka_topic, kafka_partition, kafka_offset)
		VALUES `)
	for i, rec := range records {
		if i > 0 {
			b.WriteString(",")
		}
		base := i*4 + 1
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d)", base, base+1, base+2, base+3)
		args = append(args, rec.Entry.Version, rec.Topic, rec.Partition, rec.Offset)
	}
	b.WriteString(" ON CONFLICT (version) DO NOTHING RETURNING version")

	rows, err := tx.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("insert versions: %w", err)
	}
	defer rows.Close()

	fresh := make(map[int64]struct{}, len(records))
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan inserted version: %w", err)
		}
		fresh[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("insert versions rows: %w", err)
	}
	return fresh, nil
}

// copyMutations streams rows into state_mutations via the COPY protocol.
// This path runs only for fresh versions, so replays skip it.
func copyMutations(ctx context.Context, tx *sql.Tx, records []Record) error {
	total := 0
	for _, rec := range records {
		for _, ncs := range rec.Entry.Changesets {
			total += len(ncs.Changeset.Pairs)
		}
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

	for _, rec := range records {
		version := rec.Entry.Version
		for _, ncs := range rec.Entry.Changesets {
			for _, p := range ncs.Changeset.Pairs {
				if _, err := stmt.ExecContext(ctx, ncs.Name, p.Key, version, p.Value, p.Delete); err != nil {
					return fmt.Errorf("copy mutation row: %w", err)
				}
			}
		}
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("flush copy: %w", err)
	}
	return nil
}

func insertUpgrades(ctx context.Context, tx *sql.Tx, records []Record) error {
	for _, rec := range records {
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
	}
	return nil
}
