package receipthash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

const (
	// receiptHashDomain is the domain separation tag that leads the receipt hash preimage.
	receiptHashDomain = "sei-receipts"

	// receiptHashVersion is the version of the receipt hash scheme produced by this package: the log and receipt
	// leaves, the logs and receipts trees, and the receipt hash preimage.
	receiptHashVersion uint8 = 1
)

// ComputeReceiptHash returns the receipt hash committing to a block's receipts. It returns an error if a receipt or
// log is nil, a status is neither 0 nor 1, a log has more than 4 topics, or the transaction indices are not strictly
// increasing.
func ComputeReceiptHash(
	// The receipts of the block's transactions that consumed a nonce, in block order. Each TransactionIndex is
	// the transaction's position in the block, counting every transaction. Only Status, TxHash,
	// TransactionIndex, GasUsed, and the Address, Topics, and Data of each log are read.
	receipts []*ethtypes.Receipt,
) (apphash.ReceiptHash, error) {
	leafHashes, err := receiptLeafHashes(receipts)
	if err != nil {
		return apphash.ReceiptHash{}, fmt.Errorf("computing receipt hash: %w", err)
	}
	return computeReceiptHash(uint64(len(receipts)), merkle.MerkleRoot(leafHashes)), nil
}

// computeReceiptHash returns the receipt hash committing to a receipts tree.
func computeReceiptHash(
	// The number of leaves in the receipts tree.
	leafCount uint64,
	// The root of the receipts tree.
	treeRoot [32]byte,
) apphash.ReceiptHash {
	preimage := make([]byte, 0, len(receiptHashDomain)+1+8+32)
	preimage = append(preimage, receiptHashDomain...)
	preimage = append(preimage, receiptHashVersion)
	preimage = binary.BigEndian.AppendUint64(preimage, leafCount)
	preimage = append(preimage, treeRoot[:]...)
	return sha256.Sum256(preimage)
}
