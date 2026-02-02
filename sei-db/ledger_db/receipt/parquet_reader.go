//go:build duckdb
// +build duckdb

package receipt

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/ethereum/go-ethereum/common"
)

type parquetReader struct {
	db                 *sql.DB
	basePath           string
	mu                 sync.RWMutex
	closedReceiptFiles []string
	closedLogFiles     []string
}

func newParquetReader(basePath string) (*parquetReader, error) {
	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DuckDB connector: %w", err)
	}

	db := sql.OpenDB(connector)
	numCPU := runtime.NumCPU()
	db.SetMaxOpenConns(numCPU * 2)
	db.SetMaxIdleConns(numCPU)

	_, _ = db.Exec(fmt.Sprintf("SET threads TO %d", numCPU))
	_, _ = db.Exec("SET memory_limit = '1GB'")
	_, _ = db.Exec("SET enable_object_cache = true")
	_, _ = db.Exec("SET parquet_metadata_cache_size = 500")
	_, _ = db.Exec("SET access_mode = 'READ_ONLY'")
	_, _ = db.Exec("SET enable_progress_bar = false")
	_, _ = db.Exec("SET preserve_insertion_order = false")

	reader := &parquetReader{
		db:       db,
		basePath: basePath,
	}
	reader.scanExistingFiles()
	return reader, nil
}

func (r *parquetReader) Close() error {
	return r.db.Close()
}

func (r *parquetReader) scanExistingFiles() {
	r.mu.Lock()
	defer r.mu.Unlock()

	receiptFiles, _ := filepath.Glob(filepath.Join(r.basePath, "receipts_*.parquet"))
	r.closedReceiptFiles, _ = r.validateFiles(receiptFiles)

	logFiles, _ := filepath.Glob(filepath.Join(r.basePath, "logs_*.parquet"))
	r.closedLogFiles, _ = r.validateFiles(logFiles)
}

func (r *parquetReader) validateFiles(files []string) ([]string, string) {
	if len(files) == 0 {
		return nil, ""
	}

	sort.Slice(files, func(i, j int) bool {
		return extractBlockNumber(files[i]) < extractBlockNumber(files[j])
	})

	lastFile := files[len(files)-1]
	if r.isFileReadable(lastFile) {
		return files, ""
	}
	return files[:len(files)-1], lastFile
}

func (r *parquetReader) isFileReadable(path string) bool {
	// #nosec G201 -- path comes from validated local files, not user input
	_, err := r.db.Exec(fmt.Sprintf("SELECT 1 FROM read_parquet('%s') LIMIT 1", path))
	return err == nil
}

func (r *parquetReader) onFileRotation(closedFileStartBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	receiptFile := filepath.Join(r.basePath, fmt.Sprintf("receipts_%d.parquet", closedFileStartBlock))
	logFile := filepath.Join(r.basePath, fmt.Sprintf("logs_%d.parquet", closedFileStartBlock))
	r.closedReceiptFiles = append(r.closedReceiptFiles, receiptFile)
	r.closedLogFiles = append(r.closedLogFiles, logFile)
}

func (r *parquetReader) closedReceiptFileCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.closedReceiptFiles)
}

type parquetFilePair struct {
	receiptFile string
	logFile     string
	startBlock  uint64
}

// getFilesBeforeBlock returns files whose start block is before the given block.
// These files contain only data older than the prune threshold.
func (r *parquetReader) getFilesBeforeBlock(pruneBeforeBlock uint64) []parquetFilePair {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []parquetFilePair
	for _, f := range r.closedReceiptFiles {
		startBlock := extractBlockNumber(f)
		// Only prune files that are entirely before the prune threshold
		// We need to check that the NEXT file starts before pruneBeforeBlock,
		// meaning this file's data is all older than the threshold
		if startBlock+500 <= pruneBeforeBlock { // 500 = MaxBlocksPerFile
			logFile := filepath.Join(r.basePath, fmt.Sprintf("logs_%d.parquet", startBlock))
			result = append(result, parquetFilePair{
				receiptFile: f,
				logFile:     logFile,
				startBlock:  startBlock,
			})
		}
	}
	return result
}

// removeFilesBeforeBlock removes files from tracking that are before the given block.
func (r *parquetReader) removeFilesBeforeBlock(pruneBeforeBlock uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Filter receipt files
	newReceiptFiles := make([]string, 0, len(r.closedReceiptFiles))
	for _, f := range r.closedReceiptFiles {
		startBlock := extractBlockNumber(f)
		if startBlock+500 > pruneBeforeBlock {
			newReceiptFiles = append(newReceiptFiles, f)
		}
	}
	r.closedReceiptFiles = newReceiptFiles

	// Filter log files
	newLogFiles := make([]string, 0, len(r.closedLogFiles))
	for _, f := range r.closedLogFiles {
		startBlock := extractBlockNumber(f)
		if startBlock+500 > pruneBeforeBlock {
			newLogFiles = append(newLogFiles, f)
		}
	}
	r.closedLogFiles = newLogFiles
}

func (r *parquetReader) maxReceiptBlockNumber(ctx context.Context) (uint64, bool, error) {
	r.mu.RLock()
	closedFiles := r.closedReceiptFiles
	r.mu.RUnlock()
	if len(closedFiles) == 0 {
		return 0, false, nil
	}

	var parquetFiles string
	if len(closedFiles) == 1 {
		parquetFiles = fmt.Sprintf("'%s'", closedFiles[0])
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

func (r *parquetReader) getReceiptByTxHash(ctx context.Context, txHash common.Hash) (*receiptResult, error) {
	r.mu.RLock()
	closedFiles := r.closedReceiptFiles
	r.mu.RUnlock()

	if len(closedFiles) == 0 {
		return nil, nil
	}

	var parquetFiles string
	if len(closedFiles) == 1 {
		parquetFiles = fmt.Sprintf("'%s'", closedFiles[0])
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
	var rec receiptResult
	if err := row.Scan(&rec.TxHash, &rec.BlockNumber, &rec.ReceiptBytes); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query receipt: %w", err)
	}
	return &rec, nil
}

func (r *parquetReader) getLogs(ctx context.Context, filter logFilter) ([]logResult, error) {
	r.mu.RLock()
	closedFiles := r.closedLogFiles
	r.mu.RUnlock()

	if len(closedFiles) == 0 {
		return nil, nil
	}

	files := make([]string, 0, len(closedFiles))
	for _, f := range closedFiles {
		startBlock := extractBlockNumber(f)
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

func (r *parquetReader) queryLogFiles(ctx context.Context, files []string, filter logFilter) ([]logResult, error) {
	var parquetFiles string
	if len(files) == 1 {
		parquetFiles = fmt.Sprintf("'%s'", files[0])
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

	var results []logResult
	for rows.Next() {
		var log logResult
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

func extractBlockNumber(path string) uint64 {
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
		quoted[i] = fmt.Sprintf("'%s'", f)
	}
	return strings.Join(quoted, ", ")
}
