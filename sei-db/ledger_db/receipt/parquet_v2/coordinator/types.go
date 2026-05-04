package coordinator

import (
	"errors"
	"fmt"
	"math"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// ErrStoreClosed is returned when a request is made after the coordinator has
// stopped accepting work.
var ErrStoreClosed = errors.New("store closed")

// tempReceipt is one entry in the in-memory write cache, indexed by tx
// hash. It carries enough to reconstruct a ReceiptResult for reads served
// before the receipt has been flushed to a parquet file.
type tempReceipt struct {
	blockNumber  uint64
	receiptBytes []byte
}

// ReplayedBlock summarizes one block recovered from WAL replay: the block
// number and the tx hashes whose receipts were replayed in order.
type ReplayedBlock struct {
	BlockNumber uint64
	TxHashes    []common.Hash
}

// WALReceiptConverter decodes a raw WAL receipt blob into the structured
// fields the coordinator needs to re-stage it. logStartIndex carries the
// running per-block log offset so logs from earlier txs in the same block
// don't collide.
type WALReceiptConverter func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (ReplayReceipt, error)

// ReplayReceipt is one converted WAL entry: the receipt input to re-stage,
// its tx hash, the warmup record returned to the wrapper, and the log
// count consumed (used to advance logStartIndex).
type ReplayReceipt struct {
	Input    parquet.ReceiptInput
	TxHash   common.Hash
	Warmup   parquet.ReceiptRecord
	LogCount uint
}

// ReplayResult is the outcome of a successful WAL replay: warmup records
// to seed external caches, plus the per-block tx hash listing.
type ReplayResult struct {
	WarmupRecords []parquet.ReceiptRecord
	Blocks        []ReplayedBlock
}

// ReplayHooks bundles the wrapper-specific callbacks invoked during WAL
// replay at construction time. Converter decodes the raw WAL receipt blob;
// when nil, replay is skipped entirely (used by lower-level tests that
// drive replay manually). OnReplayedBlock, when non-nil, is called once
// per recovered block after its receipts have been re-applied — the
// wrapper uses this to re-populate its tx-hash index.
type ReplayHooks struct {
	Converter       WALReceiptConverter
	OnReplayedBlock func(blockNumber uint64, txHashes []common.Hash) error
}

// int64FromUint64 converts value to int64 or errors on overflow. Used at
// the boundary where block heights cross from internal uint64 storage to
// the sdk-style int64 latestVersion.
func int64FromUint64(value uint64) (int64, error) {
	if value > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("value %d overflows int64", value)
	}
	return int64(value), nil
}
