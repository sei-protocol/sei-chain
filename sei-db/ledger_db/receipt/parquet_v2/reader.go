package parquet_v2

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// Reader is the V2 DuckDB query helper. It intentionally owns no file-list
// state; callers pass explicit file snapshots to each query.
type Reader struct {
	db               *sql.DB
	basePath         string
	maxBlocksPerFile uint64
}

func NewReader(basePath string) (*Reader, error) {
	return NewReaderWithMaxBlocksPerFile(basePath, parquet.DefaultStoreConfig().MaxBlocksPerFile)
}

func NewReaderWithMaxBlocksPerFile(basePath string, maxBlocksPerFile uint64) (*Reader, error) {
	if maxBlocksPerFile == 0 {
		maxBlocksPerFile = parquet.DefaultStoreConfig().MaxBlocksPerFile
	}

	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DuckDB connector: %w", err)
	}

	db := sql.OpenDB(connector)
	numCPU := runtime.NumCPU()
	db.SetMaxOpenConns(numCPU * 2)
	db.SetMaxIdleConns(numCPU)

	settings := []string{
		fmt.Sprintf("SET threads TO %d", numCPU),
		"SET memory_limit = '1GB'",
		"SET enable_object_cache = true",
		"SET enable_progress_bar = false",
		"SET preserve_insertion_order = false",
	}
	for _, statement := range settings {
		if _, err = db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to configure duckdb (%s): %w", statement, err)
		}
	}
	if err = configureParquetMetadataCache(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Reader{
		db:               db,
		basePath:         basePath,
		maxBlocksPerFile: maxBlocksPerFile,
	}, nil
}

func (r *Reader) setMaxBlocksPerFile(maxBlocksPerFile uint64) {
	r.maxBlocksPerFile = maxBlocksPerFile
}

func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	err := r.db.Close()
	r.db = nil
	return err
}

func (r *Reader) QueryReceiptByTxHash(ctx context.Context, files []string, txHash common.Hash) (*parquet.ReceiptResult, error) {
	_ = r
	_ = ctx
	_ = files
	_ = txHash
	return nil, ErrNotImplemented
}

func (r *Reader) QueryReceiptByTxHashInBlock(ctx context.Context, files []string, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	_ = r
	_ = ctx
	_ = files
	_ = txHash
	_ = blockNumber
	return nil, ErrNotImplemented
}

func (r *Reader) QueryLogs(ctx context.Context, files []string, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	_ = r
	_ = ctx
	_ = files
	_ = filter
	return nil, ErrNotImplemented
}

func (r *Reader) MaxReceiptBlockNumber(ctx context.Context, files []string) (uint64, bool, error) {
	if len(files) == 0 {
		return 0, false, nil
	}

	var parquetFiles string
	if len(files) == 1 {
		parquetFiles = quoteSQLString(files[0])
	} else {
		parquetFiles = fmt.Sprintf("[%s]", joinQuoted(files))
	}

	// #nosec G201 -- parquetFiles derived from local file paths
	query := fmt.Sprintf("SELECT MAX(block_number) FROM read_parquet(%s, union_by_name=true)", parquetFiles)
	row := r.db.QueryRowContext(ctx, query)
	var max sql.NullInt64
	if err := row.Scan(&max); err != nil {
		return 0, false, fmt.Errorf("failed to query max block number: %w", err)
	}
	if !max.Valid {
		return 0, false, nil
	}
	if max.Int64 < 0 {
		return 0, false, fmt.Errorf("invalid negative block number: %d", max.Int64)
	}
	return uint64(max.Int64), true, nil
}

func (r *Reader) isFileReadable(path string) bool {
	// #nosec G201 -- path comes from local parquet file scans, not user input.
	_, err := r.db.Exec(fmt.Sprintf("SELECT 1 FROM read_parquet(%s) LIMIT 1", quoteSQLString(path)))
	return err == nil
}

func configureParquetMetadataCache(db *sql.DB) error {
	const sizeSetting = "SET parquet_metadata_cache_size = 500"
	if _, err := db.Exec(sizeSetting); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "unrecognized configuration parameter") {
		return fmt.Errorf("failed to configure duckdb (%s): %w", sizeSetting, err)
	}

	const toggleSetting = "SET parquet_metadata_cache = true"
	if _, err := db.Exec(toggleSetting); err != nil {
		return fmt.Errorf("failed to configure duckdb (%s): %w", toggleSetting, err)
	}

	return nil
}

func joinQuoted(files []string) string {
	quoted := make([]string, len(files))
	for i, f := range files {
		quoted[i] = quoteSQLString(f)
	}
	return strings.Join(quoted, ", ")
}

func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
