package gigasim

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// receiptWriter persists a block's receipts through the production write path. It exists only when
// receipts are enabled; the benchmark holds a nil writer otherwise and never builds a receipt.
type receiptWriter struct {
	store   receipt.ReceiptStore
	metrics *GigasimMetrics
}

// newReceiptWriter wraps the receipt store the storage manager opened.
func newReceiptWriter(store receipt.ReceiptStore, metrics *GigasimMetrics) *receiptWriter {
	return &receiptWriter{store: store, metrics: metrics}
}

// writeBlock stores every receipt a block produced, in one call, as a node does at commit.
func (w *receiptWriter) writeBlock(number int64, receipts []*evmtypes.Receipt) error {
	if len(receipts) == 0 {
		return nil
	}

	var encodedBytes int64
	records := make([]receipt.ReceiptRecord, 0, len(receipts))
	for _, rcpt := range receipts {
		// The store accepts pre-marshaled bytes, and marshaling here keeps the cost of producing them
		// attributed to the benchmark rather than to the store.
		encoded, err := rcpt.Marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal the receipt for transaction %d of block %d: %w",
				rcpt.TransactionIndex, number, err)
		}
		encodedBytes += int64(len(encoded))
		records = append(records, receipt.ReceiptRecord{
			TxHash:       common.HexToHash(rcpt.TxHashHex),
			Receipt:      rcpt,
			ReceiptBytes: encoded,
		})
	}

	if err := w.store.SetReceipts(sdk.NewContext(nil, tmproto.Header{Height: number}, false), records); err != nil {
		return fmt.Errorf("failed to write the receipts for block %d: %w", number, err)
	}
	w.metrics.ReportReceiptsWritten(int64(len(records)))
	w.metrics.ReportStoreBytesWritten(storeReceiptDB, encodedBytes)
	return nil
}
