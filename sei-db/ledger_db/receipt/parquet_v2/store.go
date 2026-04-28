package parquet_v2

import (
	"sync"

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

	requests := make(chan coordRequest)
	done := make(chan struct{})
	reader, err := NewReaderWithMaxBlocksPerFile(cfg.DBDirectory, storeCfg.MaxBlocksPerFile)
	if err != nil {
		return nil, err
	}

	c := &coordinator{
		requests:        requests,
		done:            done,
		config:          storeCfg,
		basePath:        cfg.DBDirectory,
		receiptsBuffer:  make([]parquet.ReceiptRecord, 0, 1000),
		logsBuffer:      make([]parquet.LogRecord, 0, 10000),
		tempWriteCache:  make(map[common.Hash][]tempReceipt),
		reader:          reader,
		latestVersion:   0,
		earliestVersion: 0,
	}

	s := &Store{
		requests: requests,
		done:     done,
	}

	go c.run()

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
