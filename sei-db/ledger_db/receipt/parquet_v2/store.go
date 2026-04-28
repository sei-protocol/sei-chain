package parquet_v2

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// Store is the V2 parquet receipt store facade. In the finished implementation
// it will hold only channels into the coordinator goroutine.
type Store struct {
	requests  chan coordRequest
	done      chan struct{}
	closeOnce sync.Once
}

// NewStore creates a V2 store with a live coordinator goroutine and
// stubbed request handlers.
func NewStore(cfg parquet.StoreConfig) (*Store, error) {
	storeCfg := resolveStoreConfig(cfg)

	if err := os.MkdirAll(storeCfg.DBDirectory, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create parquet base directory: %w", err)
	}

	requests := make(chan coordRequest)
	done := make(chan struct{})
	reader, err := NewReaderWithMaxBlocksPerFile(cfg.DBDirectory, storeCfg.MaxBlocksPerFile)
	if err != nil {
		return nil, err
	}
	cleanupReader := true
	defer func() {
		if cleanupReader {
			_ = reader.Close()
		}
	}()

	walDir := filepath.Join(storeCfg.DBDirectory, "parquet-wal")
	receiptWAL, err := parquet.NewWAL(walDir)
	if err != nil {
		return nil, err
	}
	cleanupWAL := true
	defer func() {
		if cleanupWAL {
			_ = receiptWAL.Close()
		}
	}()

	closedFiles, err := scanClosedFiles(storeCfg.DBDirectory, reader)
	if err != nil {
		return nil, err
	}

	c := &coordinator{
		requests:        requests,
		done:            done,
		config:          storeCfg,
		basePath:        cfg.DBDirectory,
		closedFiles:     closedFiles,
		receiptsBuffer:  make([]parquet.ReceiptRecord, 0, 1000),
		logsBuffer:      make([]parquet.LogRecord, 0, 10000),
		tempWriteCache:  make(map[common.Hash][]tempReceipt),
		reader:          reader,
		wal:             receiptWAL,
		latestVersion:   0,
		earliestVersion: 0,
	}

	receiptFiles := make([]string, 0, len(closedFiles))
	for _, f := range closedFiles {
		receiptFiles = append(receiptFiles, f.receiptPath)
	}
	if maxBlock, ok, err := reader.MaxReceiptBlockNumber(context.Background(), receiptFiles); err != nil {
		return nil, err
	} else if ok {
		latest, err := int64FromUint64(maxBlock)
		if err != nil {
			return nil, err
		}
		c.latestVersion = latest
		if maxBlock < ^uint64(0) {
			c.fileStartBlock = maxBlock + 1
		}
	}

	if storeCfg.KeepRecent > 0 && storeCfg.PruneIntervalSeconds > 0 {
		c.pruneTicker = time.NewTicker(time.Duration(storeCfg.PruneIntervalSeconds) * time.Second)
		c.pruneTick = c.pruneTicker.C
	}

	s := &Store{
		requests: requests,
		done:     done,
	}

	go c.run()
	cleanupReader = false
	cleanupWAL = false

	return s, nil
}

func resolveStoreConfig(cfg parquet.StoreConfig) parquet.StoreConfig {
	resolved := parquet.DefaultStoreConfig()
	resolved.DBDirectory = cfg.DBDirectory
	resolved.KeepRecent = cfg.KeepRecent
	resolved.PruneIntervalSeconds = cfg.PruneIntervalSeconds
	if cfg.TxIndexBackend != "" {
		resolved.TxIndexBackend = cfg.TxIndexBackend
	}
	if cfg.BlockFlushInterval > 0 {
		resolved.BlockFlushInterval = cfg.BlockFlushInterval
	}
	if cfg.MaxBlocksPerFile > 0 {
		resolved.MaxBlocksPerFile = cfg.MaxBlocksPerFile
	}
	return resolved
}

func awaitResponse[T any](s *Store, req coordRequest, resp <-chan T) (T, error) {
	var zero T

	select {
	case s.requests <- req:
	case <-s.done:
		return zero, ErrStoreClosed
	}

	select {
	case r := <-resp:
		return r, nil
	case <-s.done:
		return zero, ErrStoreClosed
	}
}

func awaitError(s *Store, req coordRequest, resp <-chan error) error {
	err, waitErr := awaitResponse(s, req, resp)
	if waitErr != nil {
		return waitErr
	}
	return err
}
