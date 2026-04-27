package parquet_v2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/ethereum/go-ethereum/common"
)

// Reader is a stateless DuckDB-backed query helper for parquet receipt/log
// files. Unlike the v1 reader, it does NOT track which files are "closed" and
// "tracked" — that metadata is owned by the coordinator. Callers pass an
// explicit list of file paths into each query.
//
// This shape is intentional: future read workers will receive a list of files
// from the coordinator and run queries against them with no shared state. The
// coordinator decides which files are safe/legal to read.
type Reader struct {
	db *sql.DB
}

// FilePair represents a matched pair of receipt and log parquet files.
type FilePair struct {
	ReceiptFile string
	LogFile     string
	StartBlock  uint64
}

// NewReader creates a new parquet reader.
func NewReader() (*Reader, error) {
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

	return &Reader{db: db}, nil
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

// Close closes the reader.
func (r *Reader) Close() error {
	return r.db.Close()
}

// IsFileReadable reports whether DuckDB can read a parquet file (i.e. it has a
// valid footer). Used by the coordinator at startup to detect files that were
// not properly closed before a crash.
func (r *Reader) IsFileReadable(path string) bool {
	// #nosec G201 -- path comes from validated local files, not user input
	_, err := r.db.Exec(fmt.Sprintf("SELECT 1 FROM read_parquet(%s) LIMIT 1", quoteSQLString(path)))
	return err == nil
}

// MaxReceiptBlockNumber returns the maximum block number across the given
// receipt parquet files.
func (r *Reader) MaxReceiptBlockNumber(ctx context.Context, files []string) (uint64, bool, error) {
	if len(files) == 0 {
		return 0, false, nil
	}

	parquetFiles := formatFileList(files)

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

// GetReceiptByTxHash queries for a receipt by transaction hash across the
// given files. The coordinator decides which files are safe to read.
func (r *Reader) GetReceiptByTxHash(ctx context.Context, txHash common.Hash, files []string) (*ReceiptResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	parquetFiles := formatFileList(files)

	// #nosec G201 -- parquetFiles derived from local file paths
	query := fmt.Sprintf(`
		SELECT
			tx_hash, block_number, receipt_bytes
		FROM read_parquet(%s, union_by_name=true)
		WHERE tx_hash = $1
		LIMIT 1
	`, parquetFiles)

	row := r.db.QueryRowContext(ctx, query, txHash[:])
	var rec ReceiptResult
	if err := row.Scan(&rec.TxHash, &rec.BlockNumber, &rec.ReceiptBytes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query receipt: %w", err)
	}
	return &rec, nil
}

// GetLogs queries logs matching the given filter across the provided files.
func (r *Reader) GetLogs(ctx context.Context, files []string, filter LogFilter) ([]LogResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	parquetFiles := formatFileList(files)

	// #nosec G201 -- parquetFiles derived from local file paths
	query := fmt.Sprintf(`
		SELECT
			block_number, tx_hash, tx_index, log_index, address,
			topic0, topic1, topic2, topic3, data, block_hash, removed
		FROM read_parquet(%s, union_by_name=true)
		WHERE 1=1
	`, parquetFiles)

	var args []any
	argIdx := 1

	if filter.FromBlock != nil {
		query += fmt.Sprintf(" AND block_number >= $%d", argIdx)
		args = append(args, *filter.FromBlock)
		argIdx++
	}

	if filter.ToBlock != nil {
		query += fmt.Sprintf(" AND block_number <= $%d", argIdx)
		args = append(args, *filter.ToBlock)
		argIdx++
	}

	if len(filter.Addresses) > 0 {
		placeholders := make([]string, len(filter.Addresses))
		for i, addr := range filter.Addresses {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, addr[:])
			argIdx++
		}
		query += fmt.Sprintf(" AND address IN (%s)", strings.Join(placeholders, ", "))
	}

	topicCols := []string{"topic0", "topic1", "topic2", "topic3"}
	for i, topicList := range filter.Topics {
		if i >= 4 {
			break
		}
		if len(topicList) == 0 {
			continue
		}

		if len(topicList) == 1 {
			query += fmt.Sprintf(" AND %s = $%d", topicCols[i], argIdx)
			args = append(args, topicList[0][:])
			argIdx++
		} else {
			placeholders := make([]string, len(topicList))
			for j, topic := range topicList {
				placeholders[j] = fmt.Sprintf("$%d", argIdx)
				args = append(args, topic[:])
				argIdx++
			}
			query += fmt.Sprintf(" AND %s IN (%s)", topicCols[i], strings.Join(placeholders, ", "))
		}
	}

	query += " ORDER BY block_number, tx_index, log_index"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []LogResult
	for rows.Next() {
		var lr LogResult
		if err := rows.Scan(
			&lr.BlockNumber, &lr.TxHash, &lr.TxIndex, &lr.LogIndex,
			&lr.Address, &lr.Topic0, &lr.Topic1, &lr.Topic2, &lr.Topic3,
			&lr.Data, &lr.BlockHash, &lr.Removed,
		); err != nil {
			return nil, fmt.Errorf("failed to scan log: %w", err)
		}
		results = append(results, lr)
	}

	return results, rows.Err()
}

// ScanExistingFiles enumerates and validates parquet files in basePath. It
// returns sorted receipt/log file lists that are safe to query. The last file
// in each list is checked for readability; if a file is missing its parquet
// footer (typically from an unclean shutdown) the file and its counterpart are
// removed from disk.
//
// This is intended to be called once during coordinator init.
func (r *Reader) ScanExistingFiles(basePath string) (receiptFiles, logFiles []string) {
	receiptFiles = r.validateAndCleanFiles(basePath, getAllParquetFilesByPrefix(basePath, "receipts"), "logs")
	logFiles = r.validateAndCleanFiles(basePath, getAllParquetFilesByPrefix(basePath, "logs"), "receipts")
	return receiptFiles, logFiles
}

func getAllParquetFilesByPrefix(basePath, prefix string) []string {
	pattern := filepath.Join(basePath, prefix+"_*.parquet")
	files, err := filepath.Glob(pattern)
	if err != nil {
		log.Printf("failed to glob %s parquet files with pattern %q: %v", prefix, pattern, err)
	}
	return files
}

// validateAndCleanFiles checks the last file for readability. If it is corrupt
// (e.g. missing parquet footer from an unclean shutdown), the file and its
// counterpart are deleted from disk so they cannot poison future DuckDB
// queries. Only the last file needs checking because all previously rotated
// files had their writers properly closed.
func (r *Reader) validateAndCleanFiles(basePath string, files []string, counterpartPrefix string) []string {
	if len(files) == 0 {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return ExtractBlockNumber(files[i]) < ExtractBlockNumber(files[j])
	})

	lastFile := files[len(files)-1]
	if r.IsFileReadable(lastFile) {
		return files
	}

	startBlock := ExtractBlockNumber(lastFile)
	log.Printf("removing corrupt parquet file: %s", lastFile)
	_ = os.Remove(lastFile)

	counterpart := filepath.Join(basePath, fmt.Sprintf("%s_%d.parquet", counterpartPrefix, startBlock))
	_ = os.Remove(counterpart)

	return files[:len(files)-1]
}

// FilesBeforeBlock returns receipt/log file pairs whose data is entirely older
// than pruneBeforeBlock, given the configured rotation interval. The
// coordinator owns the closed file list and calls this against its own
// snapshot.
func FilesBeforeBlock(closedReceiptFiles []string, basePath string, maxBlocksPerFile, pruneBeforeBlock uint64) []FilePair {
	if maxBlocksPerFile == 0 {
		return nil
	}

	var result []FilePair
	for _, f := range closedReceiptFiles {
		startBlock := ExtractBlockNumber(f)
		// Only prune files that are entirely before the prune threshold:
		// the next rotation boundary must still be at or before pruneBeforeBlock.
		if startBlock+maxBlocksPerFile <= pruneBeforeBlock {
			logFile := filepath.Join(basePath, fmt.Sprintf("logs_%d.parquet", startBlock))
			result = append(result, FilePair{
				ReceiptFile: f,
				LogFile:     logFile,
				StartBlock:  startBlock,
			})
		}
	}
	return result
}

// FileForBlock returns the receipt parquet file path that should contain
// blockNumber, scanning the sorted closed file list. Returns "" if none.
func FileForBlock(closedReceiptFiles []string, blockNumber uint64) string {
	var best string
	for _, f := range closedReceiptFiles {
		start := ExtractBlockNumber(f)
		if start <= blockNumber {
			best = f
		} else {
			break
		}
	}
	return best
}

// FilesForLogQuery filters closedLogFiles to those that may contain logs
// matching the filter's block range.
func FilesForLogQuery(closedLogFiles []string, maxBlocksPerFile uint64, filter LogFilter) []string {
	if maxBlocksPerFile == 0 {
		return append([]string(nil), closedLogFiles...)
	}
	files := make([]string, 0, len(closedLogFiles))
	for _, f := range closedLogFiles {
		startBlock := ExtractBlockNumber(f)
		if filter.ToBlock != nil && startBlock > *filter.ToBlock {
			continue
		}
		if filter.FromBlock != nil && startBlock+maxBlocksPerFile <= *filter.FromBlock {
			continue
		}
		files = append(files, f)
	}
	return files
}

// ExtractBlockNumber extracts the block number from a parquet filename.
func ExtractBlockNumber(path string) uint64 {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".parquet")
	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return 0
	}
	num, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	return num
}

func formatFileList(files []string) string {
	if len(files) == 1 {
		return quoteSQLString(files[0])
	}
	return fmt.Sprintf("[%s]", joinQuoted(files))
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
