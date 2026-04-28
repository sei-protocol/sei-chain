package parquet_v2

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// ErrNotImplemented marks Step 1 scaffold methods that are intentionally
// non-functional until the coordinator handlers are implemented.
var ErrNotImplemented = errors.New("not implemented")

type tempReceipt struct {
	blockNumber  uint64
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
