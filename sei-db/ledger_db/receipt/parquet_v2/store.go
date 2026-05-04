package parquet_v2

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2/coordinator"
)

// Store is the public facade of the V2 parquet receipt store. It wraps a
// coordinator.Coordinator and forwards all calls to it.
//
// Store does not directly implement the receipt.ReceiptStore interface;
// the parquetReceiptStoreV2 wrapper in the parent package adapts Store to
// that interface (handling tx-hash indexing, replay glue, and the higher
// level ReceiptStore method shapes). Methods here are documented inline
// rather than via a parent interface.
type Store struct {
	coord *coordinator.Coordinator
}

// NewStore creates a V2 store backed by a live coordinator goroutine.
func NewStore(cfg parquet.StoreConfig) (*Store, error) {
	c, err := coordinator.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Store{coord: c}, nil
}

// WriteReceipts appends receipts for the block at height. height is
// authoritative; any BlockNumber on individual inputs is ignored.
func (s *Store) WriteReceipts(height uint64, inputs []parquet.ReceiptInput) error {
	return s.coord.WriteReceipts(height, inputs)
}

// GetReceiptByTxHash returns the earliest receipt for txHash, or
// (nil, nil) if none is found.
func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	return s.coord.GetReceiptByTxHash(ctx, txHash)
}

// GetReceiptByTxHashInBlock returns the receipt for txHash at exactly
// blockNumber, or (nil, nil) if no such receipt exists.
func (s *Store) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	return s.coord.GetReceiptByTxHashInBlock(ctx, txHash, blockNumber)
}

// GetLogs returns all logs matching filter across the closed log parquet
// files.
func (s *Store) GetLogs(ctx context.Context, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	return s.coord.GetLogs(ctx, filter)
}

// FileStartBlock returns the start block of the currently open parquet
// file (the next file's name will be derived from this).
func (s *Store) FileStartBlock() uint64 {
	return s.coord.FileStartBlock()
}

// LatestVersion returns the highest block height the store has observed.
func (s *Store) LatestVersion() int64 {
	return s.coord.LatestVersion()
}

// SetLatestVersion overwrites latestVersion. Used during init paths where
// the chain height is known authoritatively.
func (s *Store) SetLatestVersion(version int64) {
	s.coord.SetLatestVersion(version)
}

// SetEarliestVersion records the lowest retained block height for pruning
// bookkeeping.
func (s *Store) SetEarliestVersion(version int64) {
	s.coord.SetEarliestVersion(version)
}

// UpdateLatestVersion advances latestVersion only when version is strictly
// greater than the current value, preventing accidental rewinds.
func (s *Store) UpdateLatestVersion(version int64) {
	s.coord.UpdateLatestVersion(version)
}

// CacheRotateInterval returns the cache rotation interval (configured
// MaxBlocksPerFile) used by the wrapper to manage warmup state.
func (s *Store) CacheRotateInterval() uint64 {
	return s.coord.CacheRotateInterval()
}

// Flush forces buffered receipts/logs in the open parquet file to disk.
func (s *Store) Flush() error {
	return s.coord.Flush()
}

// Close performs a graceful shutdown, flushing and closing all writers,
// the WAL, and the reader.
func (s *Store) Close() error {
	return s.coord.Close()
}

// SimulateCrash drops in-memory writer state without flushing. Test-only;
// used to exercise WAL recovery in the surrounding test suite.
func (s *Store) SimulateCrash() {
	s.coord.SimulateCrash()
}

// SetBlockFlushInterval updates how often (in blocks) the open writers are
// flushed to disk.
func (s *Store) SetBlockFlushInterval(interval uint64) {
	s.coord.SetBlockFlushInterval(interval)
}

// SetMaxBlocksPerFile updates the rotation interval and propagates it to
// the reader.
func (s *Store) SetMaxBlocksPerFile(n uint64) {
	s.coord.SetMaxBlocksPerFile(n)
}

// SetFaultHooks installs the supplied test hooks. nil disables all hook
// checks.
func (s *Store) SetFaultHooks(hooks *parquet.FaultHooks) {
	s.coord.SetFaultHooks(hooks)
}

// ReplayWAL drives WAL replay using converter to decode receipt bytes,
// returning the recovered records and per-block tx hashes.
func (s *Store) ReplayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	return s.coord.ReplayWAL(converter)
}
