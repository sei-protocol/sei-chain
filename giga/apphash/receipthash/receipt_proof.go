package receipthash

import (
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

// ReceiptProof proves that a receipt is a leaf of the receipts tree under a receipt hash. A ReceiptProof is
// immutable; create one with BuildReceiptProof().
type ReceiptProof struct {
	// The proven receipt's fields, other than its logs root.
	fields receiptFields

	// The root of the proven receipt's logs tree.
	logsRoot [32]byte

	// The number of receipts in the receipts tree.
	count uint64

	// The receipt's position in the receipts tree.
	index uint64

	// The sibling hashes on the path from the receipt's leaf to the root of the receipts tree, leaf end first.
	siblings [][32]byte
}

// LogProof proves that a log was emitted by a receipt of the receipts tree under a receipt hash. A LogProof is
// immutable; create one with BuildLogProof().
type LogProof struct {
	// The account that emitted the proven log.
	address common.Address

	// The proven log's topics.
	topics []common.Hash

	// The proven log's data.
	data []byte

	// The log's position in its receipt's logs tree.
	logIndex uint64

	// The sibling hashes on the path from the log's leaf to the root of its receipt's logs tree, leaf end first.
	logSiblings [][32]byte

	// The fields of the receipt that emitted the log, other than its logs root.
	receiptFields receiptFields

	// The number of receipts in the receipts tree.
	receiptCount uint64

	// The receipt's position in the receipts tree.
	receiptIndex uint64

	// The sibling hashes on the path from the receipt's leaf to the root of the receipts tree, leaf end first.
	receiptSiblings [][32]byte
}

// BuildReceiptProof returns the proof of one of a block's receipts. It returns an error if index is out of range, or
// if ComputeReceiptHash() would reject the receipts. Each call rehashes every receipt, so it costs as much as
// ComputeReceiptHash().
func BuildReceiptProof(
	// The block's receipts, as passed to ComputeReceiptHash().
	receipts []*ethtypes.Receipt,
	// The position of the receipt to prove among receipts.
	index int,
) (*ReceiptProof, error) {
	leafHashes, err := receiptLeafHashes(receipts)
	if err != nil {
		return nil, fmt.Errorf("building receipt proof: %w", err)
	}
	if index < 0 || index >= len(receipts) {
		return nil, fmt.Errorf("receipt %d is out of range for %d receipts", index, len(receipts))
	}
	receipt := receipts[index]
	logHashes, _ := logLeafHashes(receipt.Logs, nil)
	return &ReceiptProof{
		fields:   newReceiptFields(receipt),
		logsRoot: merkle.MerkleRoot(logHashes),
		count:    uint64(len(receipts)),
		index:    uint64(index), //nolint:gosec // G115 - a slice index is never negative
		siblings: merkle.MerkleSiblings(leafHashes, index),
	}, nil
}

// BuildLogProof returns the proof of one log of one of a block's receipts. It returns an error if an index is out of
// range, or if ComputeReceiptHash() would reject the receipts. Each call rehashes every receipt, so it costs as much
// as ComputeReceiptHash().
func BuildLogProof(
	// The block's receipts, as passed to ComputeReceiptHash().
	receipts []*ethtypes.Receipt,
	// The position of the receipt that emitted the log among receipts.
	receiptIndex int,
	// The position of the log among the receipt's logs.
	logIndex int,
) (*LogProof, error) {
	leafHashes, err := receiptLeafHashes(receipts)
	if err != nil {
		return nil, fmt.Errorf("building log proof: %w", err)
	}
	if receiptIndex < 0 || receiptIndex >= len(receipts) {
		return nil, fmt.Errorf("receipt %d is out of range for %d receipts", receiptIndex, len(receipts))
	}
	receipt := receipts[receiptIndex]
	if logIndex < 0 || logIndex >= len(receipt.Logs) {
		return nil, fmt.Errorf("log %d is out of range for receipt %d's %d logs",
			logIndex, receiptIndex, len(receipt.Logs))
	}
	log := receipt.Logs[logIndex]
	logHashes, _ := logLeafHashes(receipt.Logs, nil)
	return &LogProof{
		address:         log.Address,
		topics:          slices.Clone(log.Topics),
		data:            slices.Clone(log.Data),
		logIndex:        uint64(logIndex), //nolint:gosec // G115 - a slice index is never negative
		logSiblings:     merkle.MerkleSiblings(logHashes, logIndex),
		receiptFields:   newReceiptFields(receipt),
		receiptCount:    uint64(len(receipts)),
		receiptIndex:    uint64(receiptIndex), //nolint:gosec // G115 - a slice index is never negative
		receiptSiblings: merkle.MerkleSiblings(leafHashes, receiptIndex),
	}, nil
}

// ComputeReceiptHash returns the receipt hash of the receipts tree the proof places its receipt in. The proof shows
// that a block produced the receipt only when the result equals that block's receipt hash, taken from a trusted
// source.
func (p *ReceiptProof) ComputeReceiptHash() apphash.ReceiptHash {
	leafHash := merkle.MerkleLeafHash(p.fields.appendLeaf(nil, p.logsRoot))
	return computeReceiptHash(p.count, merkle.MerkleRootFromSiblings(p.count, p.index, leafHash, p.siblings))
}

// TxHash returns the hash of the proven receipt's transaction.
func (p *ReceiptProof) TxHash() common.Hash {
	return p.fields.txHash
}

// BlockIndex returns the proven receipt's transaction's position in the block, counting every transaction.
func (p *ReceiptProof) BlockIndex() uint64 {
	return p.fields.blockIndex
}

// Status returns 1 if the proven receipt's transaction succeeded, and 0 if it failed.
func (p *ReceiptProof) Status() uint8 {
	return p.fields.status
}

// GasUsed returns the gas used by the proven receipt's transaction alone.
func (p *ReceiptProof) GasUsed() uint64 {
	return p.fields.gasUsed
}

// LogCount returns the number of logs the proven receipt's transaction emitted.
func (p *ReceiptProof) LogCount() uint64 {
	return p.fields.logCount
}

// Index returns the proven receipt's position among the block's receipts.
func (p *ReceiptProof) Index() uint64 {
	return p.index
}

// ComputeReceiptHash returns the receipt hash of the receipts tree the proof places its log's receipt in. The proof
// shows that a block's transaction emitted the log only when the result equals that block's receipt hash, taken from
// a trusted source.
func (p *LogProof) ComputeReceiptHash() apphash.ReceiptHash {
	logLeafHash := merkle.MerkleLeafHash(appendLogLeaf(nil, p.address, p.topics, p.data))
	logsRoot := merkle.MerkleRootFromSiblings(p.receiptFields.logCount, p.logIndex, logLeafHash, p.logSiblings)
	receiptLeafHash := merkle.MerkleLeafHash(p.receiptFields.appendLeaf(nil, logsRoot))
	receiptsRoot := merkle.MerkleRootFromSiblings(
		p.receiptCount, p.receiptIndex, receiptLeafHash, p.receiptSiblings)
	return computeReceiptHash(p.receiptCount, receiptsRoot)
}

// Address returns the account that emitted the proven log.
func (p *LogProof) Address() common.Address {
	return p.address
}

// Topics returns the proven log's topics. The caller must not mutate them.
func (p *LogProof) Topics() []common.Hash {
	return p.topics
}

// Data returns the proven log's data. The caller must not mutate it.
func (p *LogProof) Data() []byte {
	return p.data
}

// LogIndex returns the proven log's position among its receipt's logs.
func (p *LogProof) LogIndex() uint64 {
	return p.logIndex
}

// TxHash returns the hash of the transaction that emitted the proven log.
func (p *LogProof) TxHash() common.Hash {
	return p.receiptFields.txHash
}

// BlockIndex returns the position in the block of the transaction that emitted the proven log, counting every
// transaction.
func (p *LogProof) BlockIndex() uint64 {
	return p.receiptFields.blockIndex
}
