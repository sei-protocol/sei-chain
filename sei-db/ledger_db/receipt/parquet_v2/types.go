package parquet_v2

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// ErrNotImplemented marks methods that are intentionally non-functional until
// their coordinator handlers are implemented.
var ErrNotImplemented = errors.New("not implemented")

// ErrStoreClosed is returned when a request is made after the coordinator has
// stopped accepting work.
var ErrStoreClosed = errors.New("store closed")

type tempReceipt struct {
	blockNumber  uint64
	writeOrdinal uint64
	receiptBytes []byte
}

type ReplayedBlock struct {
	BlockNumber uint64
	TxHashes    []common.Hash
}

type WALReceiptConverter func(blockNumber uint64, receiptBytes []byte, logStartIndex uint) (ReplayReceipt, error)

type ReplayReceipt struct {
	Input    parquet.ReceiptInput
	TxHash   common.Hash
	Warmup   parquet.ReceiptRecord
	LogCount uint
}

type ReplayResult struct {
	WarmupRecords []parquet.ReceiptRecord
	Blocks        []ReplayedBlock
}
