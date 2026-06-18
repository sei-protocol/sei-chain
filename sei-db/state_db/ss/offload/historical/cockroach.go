package historical

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// FollowerReadStaleness>0 routes reads to any replica via AS OF SYSTEM TIME;
// 0 means strongly-consistent reads.
type CockroachConfig struct {
	DSN                   string
	MaxOpenConns          int
	MaxIdleConns          int
	ConnMaxLifetime       time.Duration
	FollowerReadStaleness time.Duration
}

func (c *CockroachConfig) ApplyDefaults() {
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 16
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
	if c.FollowerReadStaleness < 0 {
		return fmt.Errorf("follower read staleness must be non-negative")
	}
	return nil
}

// AOST is inlined into hasSQL/getSQL/batchSQL when staleness>0 so each read
// is a single implicit transaction instead of BEGIN+SET+SELECT+COMMIT.
type cockroachReader struct {
	db        *sql.DB
	staleness time.Duration

	hasSQL   string
	getSQL   string
	batchSQL string
}

var _ Reader = (*cockroachReader)(nil)

// NewCockroachReader assumes schema.sql has already been applied.
func NewCockroachReader(cfg CockroachConfig) (Reader, error) {
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

	return &cockroachReader{
		db:        db,
		staleness: cfg.FollowerReadStaleness,
		hasSQL:    inlineAOSTPointLookup(hasLookupSQL, cfg.FollowerReadStaleness),
		getSQL:    inlineAOSTPointLookup(getLookupSQL, cfg.FollowerReadStaleness),
		batchSQL:  inlineAOSTBatchLookup(batchLookupSQL, cfg.FollowerReadStaleness),
	}, nil
}

func (r *cockroachReader) Close() error { return r.db.Close() }

func (r *cockroachReader) LastVersion(ctx context.Context) (int64, error) {
	var v sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT max(version) FROM state_versions`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("read last version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Int64, nil
}

func (r *cockroachReader) Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error) {
	var alive bool
	err := r.db.QueryRowContext(ctx, r.hasSQL, storeName, key, targetVersion).Scan(&alive)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("has lookup: %w", err)
	}
	return alive, nil
}

func (r *cockroachReader) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	var (
		version int64
		bz      []byte
		deleted bool
	)
	err := r.db.QueryRowContext(ctx, r.getSQL, storeName, key, targetVersion).Scan(&version, &bz, &deleted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Value{}, ErrNotFound
		}
		return Value{}, fmt.Errorf("get lookup: %w", err)
	}
	if deleted {
		return Value{}, ErrNotFound
	}
	return Value{Bytes: bz, Version: version}, nil
}

// LATERAL + LIMIT 1 against the descending PK turns each (store, key) pair
// into a single index seek; $1=stores, $2=keys (parallel arrays), $3=version.
const batchLookupSQL = `
SELECT t.store_name, t.key, m.version, m.value, m.deleted
FROM unnest($1::STRING[], $2::BYTES[]) AS t(store_name, key),
     LATERAL (
       SELECT version, value, deleted
       FROM state_mutations
       WHERE store_name = t.store_name
         AND key = t.key
         AND version <= $3
       ORDER BY version DESC
       LIMIT 1
     ) m`

// `NOT deleted` is inlined because tombstones at-or-below the target version
// mean the key doesn't exist.
const hasLookupSQL = `
SELECT NOT deleted
FROM state_mutations
WHERE store_name = $1 AND key = $2 AND version <= $3
ORDER BY version DESC
LIMIT 1`

const getLookupSQL = `
SELECT version, value, deleted
FROM state_mutations
WHERE store_name = $1 AND key = $2 AND version <= $3
ORDER BY version DESC
LIMIT 1`

// Per-table AOST is the form CRDB documents for follower-read SELECTs.
func inlineAOSTPointLookup(template string, staleness time.Duration) string {
	if staleness <= 0 {
		return template
	}
	return strings.Replace(template,
		"FROM state_mutations",
		"FROM state_mutations "+aostClause(staleness),
		1)
}

// CRDB rejects per-table AOST inside a subquery when the outer SELECT
// doesn't also AOST, so the LATERAL form takes the clause at the top level.
func inlineAOSTBatchLookup(template string, staleness time.Duration) string {
	if staleness <= 0 {
		return template
	}
	return strings.TrimRight(template, "\n ") + "\n" + aostClause(staleness)
}

func aostClause(staleness time.Duration) string {
	return fmt.Sprintf("AS OF SYSTEM TIME with_max_staleness('%s')", staleness)
}

func splitLookups(lookups []Lookup) (stores []string, keys [][]byte) {
	stores = make([]string, len(lookups))
	keys = make([][]byte, len(lookups))
	for i, l := range lookups {
		stores[i] = l.StoreName
		keys[i] = []byte(l.Key)
	}
	return stores, keys
}

func (r *cockroachReader) BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error) {
	if len(lookups) == 0 {
		return map[Lookup]Value{}, nil
	}
	stores, keys := splitLookups(lookups)
	out := make(map[Lookup]Value, len(lookups))

	rows, err := r.db.QueryContext(ctx, r.batchSQL,
		pq.StringArray(stores), pq.ByteaArray(keys), targetVersion)
	if err != nil {
		return nil, fmt.Errorf("batch lookup: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			storeName string
			key       []byte
			version   int64
			value     []byte
			deleted   bool
		)
		if err := rows.Scan(&storeName, &key, &version, &value, &deleted); err != nil {
			return nil, fmt.Errorf("scan batch row: %w", err)
		}
		if deleted {
			continue
		}
		out[Lookup{StoreName: storeName, Key: string(key)}] = Value{
			Bytes:   value,
			Version: version,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch lookup: %w", err)
	}
	return out, nil
}
