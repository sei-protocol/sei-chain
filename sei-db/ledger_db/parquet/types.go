package parquet

import "github.com/ethereum/go-ethereum/common"

// ReceiptResult holds the raw receipt data returned from a query.
type ReceiptResult struct {
	TxHash       []byte
	BlockNumber  uint64
	ReceiptBytes []byte
}

// LogFilter specifies criteria for filtering logs.
type LogFilter struct {
	FromBlock *uint64
	ToBlock   *uint64
	Addresses []common.Address
	Topics    [][]common.Hash
	Limit     int
}

// LogResult holds log data returned from a query.
type LogResult struct {
	BlockNumber uint64
	TxHash      []byte
	TxIndex     uint32
	LogIndex    uint32
	Address     []byte
	Topic0      []byte
	Topic1      []byte
	Topic2      []byte
	Topic3      []byte
	Data        []byte
	BlockHash   []byte
	Removed     bool
}

// ReceiptRecord is a parquet-specific record for storing receipts.
type ReceiptRecord struct {
	TxHash       []byte `parquet:"tx_hash"`
	BlockNumber  uint64 `parquet:"block_number"`
	ReceiptBytes []byte `parquet:"receipt_bytes"`
}

// LogRecord is a parquet-specific record for storing log entries.
type LogRecord struct {
	BlockNumber uint64 `parquet:"block_number"`
	TxHash      []byte `parquet:"tx_hash"`
	TxIndex     uint32 `parquet:"tx_index"`
	LogIndex    uint32 `parquet:"log_index"`
	Address     []byte `parquet:"address"`
	BlockHash   []byte `parquet:"block_hash"`
	Removed     bool   `parquet:"removed"`

	Topic0 []byte `parquet:"topic0"`
	Topic1 []byte `parquet:"topic1"`
	Topic2 []byte `parquet:"topic2"`
	Topic3 []byte `parquet:"topic3"`

	Data []byte `parquet:"data"`
}
