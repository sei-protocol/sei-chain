package receipthash

import (
	"fmt"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/apphash"
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
	path merklePath
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
	logPath merklePath

	// The fields of the receipt that emitted the log, other than its logs root.
	receiptFields receiptFields

	// The number of receipts in the receipts tree.
	receiptCount uint64

	// The receipt's position in the receipts tree.
	receiptPath merklePath
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
		logsRoot: merkleRoot(logHashes),
		count:    uint64(len(receipts)),
		path:     newMerklePath(leafHashes, index),
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
		address:       log.Address,
		topics:        slices.Clone(log.Topics),
		data:          slices.Clone(log.Data),
		logPath:       newMerklePath(logHashes, logIndex),
		receiptFields: newReceiptFields(receipt),
		receiptCount:  uint64(len(receipts)),
		receiptPath:   newMerklePath(leafHashes, receiptIndex),
	}, nil
}

// ComputeReceiptHash returns the receipt hash of the receipts tree the proof places its receipt in. The proof shows
// that a block produced the receipt only when the result equals that block's receipt hash, taken from a trusted
// source.
func (p *ReceiptProof) ComputeReceiptHash() apphash.ReceiptHash {
	leafHash := merkleLeafHash(p.fields.appendLeaf(nil, p.logsRoot))
	return computeReceiptHash(p.count, p.path.root(p.count, leafHash))
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
	return p.path.index
}

// ComputeReceiptHash returns the receipt hash of the receipts tree the proof places its log's receipt in. The proof
// shows that a block's transaction emitted the log only when the result equals that block's receipt hash, taken from
// a trusted source.
func (p *LogProof) ComputeReceiptHash() apphash.ReceiptHash {
	logLeafHash := merkleLeafHash(appendLogLeaf(nil, p.address, p.topics, p.data))
	logsRoot := p.logPath.root(p.receiptFields.logCount, logLeafHash)
	receiptLeafHash := merkleLeafHash(p.receiptFields.appendLeaf(nil, logsRoot))
	return computeReceiptHash(p.receiptCount, p.receiptPath.root(p.receiptCount, receiptLeafHash))
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
	return p.logPath.index
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
