package parquet_v2

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

func scanClosedFiles(basePath string, reader *Reader) ([]closedFile, error) {
	receiptFiles, err := parquetFilesByPrefix(basePath, "receipts")
	if err != nil {
		return nil, err
	}
	logFiles, err := parquetFilesByPrefix(basePath, "logs")
	if err != nil {
		return nil, err
	}

	receiptFiles = validateAndCleanFiles(basePath, reader, receiptFiles, "logs")
	logFiles = validateAndCleanFiles(basePath, reader, logFiles, "receipts")

	logByStart := make(map[uint64]string, len(logFiles))
	for _, path := range logFiles {
		if fileExists(path) {
			logByStart[parquet.ExtractBlockNumber(path)] = path
		}
	}

	closed := make([]closedFile, 0, len(receiptFiles))
	for _, receiptPath := range receiptFiles {
		if !fileExists(receiptPath) {
			continue
		}
		startBlock := parquet.ExtractBlockNumber(receiptPath)
		logPath, ok := logByStart[startBlock]
		if !ok {
			continue
		}
		closed = append(closed, closedFile{
			startBlock:  startBlock,
			receiptPath: receiptPath,
			logPath:     logPath,
		})
	}

	sort.Slice(closed, func(i, j int) bool {
		return closed[i].startBlock < closed[j].startBlock
	})
	return closed, nil
}

func parquetFilesByPrefix(basePath, prefix string) ([]string, error) {
	pattern := filepath.Join(basePath, prefix+"_*.parquet")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob %s parquet files with pattern %q: %w", prefix, pattern, err)
	}
	return files, nil
}

func validateAndCleanFiles(basePath string, reader *Reader, files []string, counterpartPrefix string) []string {
	if len(files) == 0 {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return parquet.ExtractBlockNumber(files[i]) < parquet.ExtractBlockNumber(files[j])
	})

	lastFile := files[len(files)-1]
	if reader.isFileReadable(lastFile) {
		return files
	}

	startBlock := parquet.ExtractBlockNumber(lastFile)
	_ = os.Remove(lastFile)
	counterpart := filepath.Join(basePath, fmt.Sprintf("%s_%d.parquet", counterpartPrefix, startBlock))
	_ = os.Remove(counterpart)
	return files[:len(files)-1]
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
