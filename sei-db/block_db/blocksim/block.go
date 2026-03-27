package blocksim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

type block struct {
	// The height of the block.
	height uint64

	// The (simulated) hash of the block.
	hash []byte

	// The transactions in the block.
	transactions []*transaction

	// Metadata for the block. Randomly generated.
	metadata []byte
}

// Creates a randomized block with the given height, first transaction ID, last transaction ID,
// transaction size, and metadata size.
func RandomBlock(
	height uint64,
	crand *rand.CannedRandom,
	firstTransactionID uint64,
	lastTransactionID uint64,
	transactionSize int,
	metadataSize int,
) *block {
	transactions := make([]*transaction, 0, lastTransactionID-firstTransactionID+1)
	for id := firstTransactionID; id <= lastTransactionID; id++ {
		transactions = append(transactions, RandomTransaction(id, crand, transactionSize))
	}
	metadata := crand.Bytes(metadataSize)
	hash := crand.Address(0, int64(height), 32)
	return &block{
		height:       height,
		hash:         hash,
		transactions: transactions,
		metadata:     metadata,
	}
}

// Returns the hash of the block.
//
// Data is not safe to modify in place, make a copy before modifying.
func (b *block) Hash() []byte {
	return b.hash
}

// Returns the transactions in the block.
//
// Data is not safe to modify in place, make a copy before modifying.
func (b *block) Transactions() []*transaction {
	return b.transactions
}

// Returns the metadata of the block.
//
// Data is not safe to modify in place, make a copy before modifying.
func (b *block) Metadata() []byte {
	return b.metadata
}

// Returns the height of the block.
func (b *block) Height() uint64 {
	return b.height
}

// Serialized block layout:
//
//	[8 bytes: height]
//	[4 bytes: metadata size (M)]
//	[M bytes: metadata]
//	[4 bytes: number of transactions (N)]
//	For each transaction:
//	  [4 bytes: serialized transaction size (S)]
//	  [S bytes: serialized transaction data]
func (b *block) Serialize() []byte {
	serializedTransactions := make([][]byte, 0, len(b.transactions))
	serializedTransactionsSize := 0
	for _, txn := range b.transactions {
		serializedTransaction := txn.Serialize()
		serializedTransactions = append(serializedTransactions, serializedTransaction)
		serializedTransactionsSize += 4 /* size prefix */ + len(serializedTransaction)
	}

	dataLen := 8 /* height */ + 4 /* metadata size */ + len(b.metadata) +
		4 /* number of transactions */ + serializedTransactionsSize

	data := make([]byte, dataLen)
	off := 0

	binary.BigEndian.PutUint64(data[off:], b.height)
	off += 8

	binary.BigEndian.PutUint32(data[off:], uint32(len(b.metadata)))
	off += 4

	copy(data[off:], b.metadata)
	off += len(b.metadata)

	binary.BigEndian.PutUint32(data[off:], uint32(len(b.transactions)))
	off += 4

	for _, serializedTransaction := range serializedTransactions {
		binary.BigEndian.PutUint32(data[off:], uint32(len(serializedTransaction)))
		off += 4
		copy(data[off:], serializedTransaction)
		off += len(serializedTransaction)
	}
	return data
}

func DeserializeBlock(crand *rand.CannedRandom, data []byte) (*block, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("data too short to contain a block")
	}

	off := 0

	height := binary.BigEndian.Uint64(data[off:])
	off += 8

	metadataSize := int(binary.BigEndian.Uint32(data[off:]))
	off += 4

	if len(data) < off+metadataSize+4 {
		return nil, fmt.Errorf("data too short to contain block metadata")
	}
	metadata := make([]byte, metadataSize)
	copy(metadata, data[off:off+metadataSize])
	off += metadataSize

	numberOfTransactions := int(binary.BigEndian.Uint32(data[off:]))
	off += 4

	transactions := make([]*transaction, 0, numberOfTransactions)
	for i := 0; i < numberOfTransactions; i++ {
		if len(data) < off+4 {
			return nil, fmt.Errorf("data too short to contain transaction size")
		}
		transactionSize := int(binary.BigEndian.Uint32(data[off:]))
		off += 4
		if len(data) < off+transactionSize {
			return nil, fmt.Errorf("data too short to contain transaction")
		}
		txn, err := DeserializeTransaction(crand, data[off:off+transactionSize])
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize transaction: %w", err)
		}
		off += transactionSize
		transactions = append(transactions, txn)
	}

	hash := crand.Address(0, int64(height), 32)
	return &block{
		height:       height,
		hash:         hash,
		metadata:     metadata,
		transactions: transactions,
	}, nil
}
