package receipthash

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

// receiptFields holds the fields of a receipt leaf other than its logs root.
type receiptFields struct {
	// Whether the transaction succeeded: 1 if it did, 0 if it failed.
	status uint8

	// The hash of the transaction.
	txHash common.Hash

	// The transaction's position in the block, counting every transaction.
	blockIndex uint64

	// The gas used by this transaction alone.
	gasUsed uint64

	// The number of logs the transaction emitted.
	logCount uint64
}

// newReceiptFields returns the fields of a receipt's leaf other than its logs root.
func newReceiptFields(
	// The receipt. Must have passed validateReceipt().
	receipt *ethtypes.Receipt,
) receiptFields {
	return receiptFields{
		status:     uint8(receipt.Status), //nolint:gosec // G115 - validateReceipt() bounds the status
		txHash:     receipt.TxHash,
		blockIndex: uint64(receipt.TransactionIndex),
		gasUsed:    receipt.GasUsed,
		logCount:   uint64(len(receipt.Logs)),
	}
}

// appendLeaf appends the preimage of the receipt's leaf in the receipts tree to dst and returns the extended slice.
func (f *receiptFields) appendLeaf(
	// The bytes to append to.
	dst []byte,
	// The root of the receipt's logs tree.
	logsRoot [32]byte,
) []byte {
	dst = append(dst, f.status)
	dst = append(dst, f.txHash[:]...)
	dst = binary.AppendUvarint(dst, f.blockIndex)
	dst = binary.AppendUvarint(dst, f.gasUsed)
	dst = binary.AppendUvarint(dst, f.logCount)
	return append(dst, logsRoot[:]...)
}

// receiptLeafHashes returns the leaf hash of each receipt in the receipts tree, in order. It returns an error unless
// the receipts pass validateReceipts().
func receiptLeafHashes(
	// The block's receipts, in block order.
	receipts []*ethtypes.Receipt,
) ([][32]byte, error) {
	if err := validateReceipts(receipts); err != nil {
		return nil, err
	}
	leafHashes := make([][32]byte, len(receipts))
	var scratch []byte
	for i, receipt := range receipts {
		var logHashes [][32]byte
		logHashes, scratch = logLeafHashes(receipt.Logs, scratch)
		fields := newReceiptFields(receipt)
		scratch = fields.appendLeaf(scratch[:0], merkle.MerkleRoot(logHashes))
		leafHashes[i] = merkle.MerkleLeafHash(scratch)
	}
	return leafHashes, nil
}

// validateReceipts returns an error unless every receipt passes validateReceipt() and their transaction indices are
// strictly increasing.
func validateReceipts(
	// The block's receipts, in block order.
	receipts []*ethtypes.Receipt,
) error {
	for i, receipt := range receipts {
		if err := validateReceipt(receipt); err != nil {
			return fmt.Errorf("receipt %d: %w", i, err)
		}
		if i > 0 && receipt.TransactionIndex <= receipts[i-1].TransactionIndex {
			return fmt.Errorf("receipt %d: transaction index %d does not follow transaction index %d",
				i, receipt.TransactionIndex, receipts[i-1].TransactionIndex)
		}
	}
	return nil
}

// validateReceipt returns an error unless receipt is non-nil, its status is 0 or 1, and every log passes
// validateLog().
func validateReceipt(
	// The receipt to check.
	receipt *ethtypes.Receipt,
) error {
	if receipt == nil {
		return fmt.Errorf("receipt is nil")
	}
	if receipt.Status != ethtypes.ReceiptStatusFailed && receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return fmt.Errorf("status is %d, want %d or %d",
			receipt.Status, ethtypes.ReceiptStatusFailed, ethtypes.ReceiptStatusSuccessful)
	}
	for i, log := range receipt.Logs {
		if err := validateLog(log); err != nil {
			return fmt.Errorf("log %d: %w", i, err)
		}
	}
	return nil
}
