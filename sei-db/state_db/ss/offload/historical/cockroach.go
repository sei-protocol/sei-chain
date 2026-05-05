package historical

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// FollowerReadStaleness>0 switches reads to AS OF SYSTEM TIME so any replica
// can serve them; 0 means strongly-consistent reads.
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

type cockroachReader struct {
	db        *sql.DB
	staleness time.Duration
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

	return &cockroachReader{db: db, staleness: cfg.FollowerReadStaleness}, nil
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
	rows, err := r.queryRows(ctx, hasLookupSQL, storeName, key, targetVersion)
	if err != nil {
		return false, fmt.Errorf("has lookup: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var alive bool
	if err := rows.Scan(&alive); err != nil {
		return false, fmt.Errorf("scan has row: %w", err)
	}
	return alive, nil
}

func (r *cockroachReader) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	lkp := Lookup{StoreName: storeName, Key: string(key)}
	res, err := r.BatchGet(ctx, targetVersion, []Lookup{lkp})
	if err != nil {
		return Value{}, err
	}
	v, ok := res[lkp]
	if !ok {
		return Value{}, ErrNotFound
	}
	return v, nil
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

// hasLookupSQL is the value-less Has counterpart. NOT deleted is checked
// inline because tombstones at-or-below the target mean "doesn't exist".
const hasLookupSQL = `
SELECT NOT deleted
FROM state_mutations
WHERE store_name = $1 AND key = $2 AND version <= $3
ORDER BY version DESC
LIMIT 1`

func splitLookups(lookups []Lookup) (stores []string, keys [][]byte) {
	stores = make([]string, len(lookups))
	keys = make([][]byte, len(lookups))
	for i, l := range lookups {
		stores[i] = l.StoreName
		keys[i] = []byte(l.Key)
	}
	return stores, keys
}

func aostStmt(staleness time.Duration) string {
	return fmt.Sprintf("SET TRANSACTION AS OF SYSTEM TIME with_max_staleness('%s')", staleness)
}

// queryRows runs q against r.db. When staleness>0 it wraps in a read-only TX
// to set AS OF SYSTEM TIME; otherwise it runs directly so a point read costs
// one round-trip instead of three (BEGIN + SELECT + COMMIT).
func (r *cockroachReader) queryRows(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.staleness <= 0 {
		return r.db.QueryContext(ctx, q, args...)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx, aostStmt(r.staleness)); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set follower read: %w", err)
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	// Commit on Close so the caller's defer rows.Close() releases the TX too.
	return rows, nil
}

func (r *cockroachReader) BatchGet(ctx context.Context, targetVersion int64, lookups []Lookup) (map[Lookup]Value, error) {
	if len(lookups) == 0 {
		return map[Lookup]Value{}, nil
	}
	stores, keys := splitLookups(lookups)
	out := make(map[Lookup]Value, len(lookups))

	rows, err := r.queryRows(ctx, batchLookupSQL, pq.StringArray(stores), pq.ByteaArray(keys), targetVersion)
	if err != nil {
		return nil, fmt.Errorf("batch lookup: %w", err)
	}
	defer rows.Close()
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
		return nil, err
	}
	return out, nil
}
