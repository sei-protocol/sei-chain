package receipt

import "github.com/ethereum/go-ethereum/common"

type receiptResult struct {
	TxHash       []byte
	BlockNumber  uint64
	ReceiptBytes []byte
}

type logFilter struct {
	FromBlock *uint64
	ToBlock   *uint64
	Addresses []common.Address
	Topics    [][]common.Hash
	Limit     int
}

type logResult struct {
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
