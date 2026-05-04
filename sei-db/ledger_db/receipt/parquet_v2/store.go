package parquet_v2

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2/coordinator"
)

// Store is the public facade of the V2 parquet receipt store. It wraps a
// coordinator.Coordinator and forwards all calls to it.
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

func (s *Store) WriteReceipts(height uint64, inputs []parquet.ReceiptInput) error {
	return s.coord.WriteReceipts(height, inputs)
}

func (s *Store) GetReceiptByTxHash(ctx context.Context, txHash common.Hash) (*parquet.ReceiptResult, error) {
	return s.coord.GetReceiptByTxHash(ctx, txHash)
}

func (s *Store) GetReceiptByTxHashInBlock(ctx context.Context, txHash common.Hash, blockNumber uint64) (*parquet.ReceiptResult, error) {
	return s.coord.GetReceiptByTxHashInBlock(ctx, txHash, blockNumber)
}

func (s *Store) GetLogs(ctx context.Context, filter parquet.LogFilter) ([]parquet.LogResult, error) {
	return s.coord.GetLogs(ctx, filter)
}

func (s *Store) FileStartBlock() uint64 {
	return s.coord.FileStartBlock()
}

func (s *Store) LatestVersion() int64 {
	return s.coord.LatestVersion()
}

func (s *Store) SetLatestVersion(version int64) {
	s.coord.SetLatestVersion(version)
}

func (s *Store) SetEarliestVersion(version int64) {
	s.coord.SetEarliestVersion(version)
}

func (s *Store) UpdateLatestVersion(version int64) {
	s.coord.UpdateLatestVersion(version)
}

func (s *Store) CacheRotateInterval() uint64 {
	return s.coord.CacheRotateInterval()
}

func (s *Store) Flush() error {
	return s.coord.Flush()
}

func (s *Store) Close() error {
	return s.coord.Close()
}

func (s *Store) SimulateCrash() {
	s.coord.SimulateCrash()
}

func (s *Store) SetBlockFlushInterval(interval uint64) {
	s.coord.SetBlockFlushInterval(interval)
}

func (s *Store) SetMaxBlocksPerFile(n uint64) {
	s.coord.SetMaxBlocksPerFile(n)
}

func (s *Store) SetFaultHooks(hooks *parquet.FaultHooks) {
	s.coord.SetFaultHooks(hooks)
}

func (s *Store) ReplayWAL(converter WALReceiptConverter) (ReplayResult, error) {
	return s.coord.ReplayWAL(converter)
}
