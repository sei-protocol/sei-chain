package parquet

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/ethereum/go-ethereum/common"
)

// Reader provides DuckDB-based reading of parquet files.
type Reader struct {
	db                 *sql.DB
	basePath           string
	maxBlocksPerFile   uint64
	mu                 sync.RWMutex
	closedReceiptFiles []string
	closedLogFiles     []string
}

// FilePair represents a matched pair of receipt and log parquet files.
type FilePair struct {
	ReceiptFile string
	LogFile     string
	StartBlock  uint64
}

// NewReader creates a new parquet reader for the given base path.
func NewReader(basePath string) (*Reader, error) {
	return NewReaderWithMaxBlocksPerFile(basePath, defaultMaxBlocksPerFile)
}

// NewReaderWithMaxBlocksPerFile creates a new parquet reader with a configured file span.
func NewReaderWithMaxBlocksPerFile(basePath string, maxBlocksPerFile uint64) (*Reader, error) {
	if maxBlocksPerFile == 0 {
		maxBlocksPerFile = defaultMaxBlocksPerFile
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

	reader := &Reader{
		db:               db,
		basePath:         basePath,
		maxBlocksPerFile: maxBlocksPerFile,
	}
	reader.scanExistingFiles()
	return reader, nil
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

func (r *Reader) scanExistingFiles() {
	r.mu.Lock()
	defer r.mu.Unlock()

	receiptPattern := filepath.Join(r.basePath, "receipts_*.parquet")
	receiptFiles, err := filepath.Glob(receiptPattern)
	if err != nil {
		log.Printf("failed to glob receipt parquet files with pattern %q: %v", receiptPattern, err)
	}
	r.closedReceiptFiles = r.validateFiles(receiptFiles)

	logPattern := filepath.Join(r.basePath, "logs_*.parquet")
	logFiles, err := filepath.Glob(logPattern)
	if err != nil {
		log.Printf("failed to glob log parquet files with pattern %q: %v", logPattern, err)
	}
	r.closedLogFiles = r.validateFiles(logFiles)
}

func (r *Reader) validateFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return ExtractBlockNumber(files[i]) < ExtractBlockNumber(files[j])
	})

	lastFile := files[len(files)-1]
	if r.isFileReadable(lastFile) {
		return files
	}
	return files[:len(files)-1]
}

func (r *Reader) isFileReadable(path string) bool {
	// #nosec G201 -- path comes from validated local files, not user input
	_, err := r.db.Exec(fmt.Sprintf("SELECT 1 FROM read_parquet(%s) LIMIT 1", quoteSQLString(path)))
	return err == nil
}

// OnFileRotation notifies the reader that a file has been rotated.
func (r *Reader) OnFileRotation(closedFileStartBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	receiptFile := filepath.Join(r.basePath, fmt.Sprintf("receipts_%d.parquet", closedFileStartBlock))
	logFile := filepath.Join(r.basePath, fmt.Sprintf("logs_%d.parquet", closedFileStartBlock))
	r.closedReceiptFiles = append(r.closedReceiptFiles, receiptFile)
	r.closedLogFiles = append(r.closedLogFiles, logFile)
}

// ClosedReceiptFileCount returns the number of closed receipt files.
func (r *Reader) ClosedReceiptFileCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.closedReceiptFiles)
}

// GetFilesBeforeBlock returns files whose start block is before the given block.
// These files contain only data older than the prune threshold.
func (r *Reader) GetFilesBeforeBlock(pruneBeforeBlock uint64) []FilePair {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []FilePair
	for _, f := range r.closedReceiptFiles {
		startBlock := ExtractBlockNumber(f)
		// Only prune files that are entirely before the prune threshold
		// We need to check that the NEXT file starts before pruneBeforeBlock,
		// meaning this file's data is all older than the threshold
		if startBlock+r.maxBlocksPerFile <= pruneBeforeBlock {
			logFile := filepath.Join(r.basePath, fmt.Sprintf("logs_%d.parquet", startBlock))
			result = append(result, FilePair{
				ReceiptFile: f,
				LogFile:     logFile,
				StartBlock:  startBlock,
			})
		}
	}
	return result
}

// RemoveTrackedReceiptFile removes a specific receipt file from reader tracking.
func (r *Reader) RemoveTrackedReceiptFile(startBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newFiles := make([]string, 0, len(r.closedReceiptFiles))
	for _, f := range r.closedReceiptFiles {
		if ExtractBlockNumber(f) != startBlock {
			newFiles = append(newFiles, f)
		}
	}
	r.closedReceiptFiles = newFiles
}

// RemoveTrackedLogFile removes a specific log file from reader tracking.
func (r *Reader) RemoveTrackedLogFile(startBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	newFiles := make([]string, 0, len(r.closedLogFiles))
	for _, f := range r.closedLogFiles {
		if ExtractBlockNumber(f) != startBlock {
			newFiles = append(newFiles, f)
		}
	}
	r.closedLogFiles = newFiles
}

// AddTrackedReceiptFile adds a specific receipt file to reader tracking if missing.
func (r *Reader) AddTrackedReceiptFile(startBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := filepath.Join(r.basePath, fmt.Sprintf("receipts_%d.parquet", startBlock))
	for _, f := range r.closedReceiptFiles {
		if f == path {
			return
		}
	}
	r.closedReceiptFiles = append(r.closedReceiptFiles, path)
	sort.Slice(r.closedReceiptFiles, func(i, j int) bool {
		return ExtractBlockNumber(r.closedReceiptFiles[i]) < ExtractBlockNumber(r.closedReceiptFiles[j])
	})
}

// AddTrackedLogFile adds a specific log file to reader tracking if missing.
func (r *Reader) AddTrackedLogFile(startBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	path := filepath.Join(r.basePath, fmt.Sprintf("logs_%d.parquet", startBlock))
	for _, f := range r.closedLogFiles {
		if f == path {
			return
		}
	}
	r.closedLogFiles = append(r.closedLogFiles, path)
	sort.Slice(r.closedLogFiles, func(i, j int) bool {
		return ExtractBlockNumber(r.closedLogFiles[i]) < ExtractBlockNumber(r.closedLogFiles[j])
	})
}

// MaxReceiptBlockNumber returns the maximum block number in the receipt files.
func (r *Reader) MaxReceiptBlockNumber(ctx context.Context) (uint64, bool, error) {
	r.mu.RLock()
	closedFiles := r.closedReceiptFiles
	r.mu.RUnlock()
	if len(closedFiles) == 0 {
		return 0, false, nil
	}

	var parquetFiles string
	if len(closedFiles) == 1 {
		parquetFiles = quoteSQLString(closedFiles[0])
	} else {
		parquetFiles = fmt.Sprintf("[%s]", joinQuoted(closedFiles))
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

// GetReceiptByTxHash queries for a receipt by transaction hash.
func (r *Reader) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*ReceiptResult, error) {
	r.mu.RLock()
	closedFiles := r.closedReceiptFiles
	r.mu.RUnlock()

	if len(closedFiles) == 0 {
		return nil, nil
	}

	var parquetFiles string
	if len(closedFiles) == 1 {
		parquetFiles = quoteSQLString(closedFiles[0])
	} else {
		parquetFiles = fmt.Sprintf("[%s]", joinQuoted(closedFiles))
	}

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
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query receipt: %w", err)
	}
	return &rec, nil
}

// GetLogs queries logs matching the given filter.
func (r *Reader) GetLogs(ctx context.Context, filter LogFilter) ([]LogResult, error) {
	r.mu.RLock()
	closedFiles := r.closedLogFiles
	r.mu.RUnlock()

	if len(closedFiles) == 0 {
		return nil, nil
	}

	files := make([]string, 0, len(closedFiles))
	for _, f := range closedFiles {
		startBlock := ExtractBlockNumber(f)
		if filter.ToBlock != nil && startBlock > *filter.ToBlock {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil
	}

	return r.queryLogFiles(ctx, files, filter)
}

func (r *Reader) queryLogFiles(ctx context.Context, files []string, filter LogFilter) ([]LogResult, error) {
	var parquetFiles string
	if len(files) == 1 {
		parquetFiles = quoteSQLString(files[0])
	} else {
		parquetFiles = fmt.Sprintf("[%s]", joinQuoted(files))
	}

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
		var log LogResult
		if err := rows.Scan(
			&log.BlockNumber, &log.TxHash, &log.TxIndex, &log.LogIndex,
			&log.Address, &log.Topic0, &log.Topic1, &log.Topic2, &log.Topic3,
			&log.Data, &log.BlockHash, &log.Removed,
		); err != nil {
			return nil, fmt.Errorf("failed to scan log: %w", err)
		}
		results = append(results, log)
	}

	return results, rows.Err()
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
