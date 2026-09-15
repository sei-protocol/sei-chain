package gigasim

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// receiptWriter persists a block's receipts through the production write path. It exists only when
// receipts are enabled; the benchmark holds a nil writer otherwise and never builds a receipt.
type receiptWriter struct {
	store receipt.ReceiptStore

	metrics *GigasimMetrics
}

// newReceiptWriter wraps the receipt store the storage manager opened.
func newReceiptWriter(store receipt.ReceiptStore, gigasimMetrics *GigasimMetrics) *receiptWriter {
	return &receiptWriter{
		store:   store,
		metrics: gigasimMetrics,
	}
}

// writeBlock stores every receipt a block produced, in one call, as a node does at commit.
//
// A block that produced none is still written. The store stamps its height for an empty batch, and
// skipping the call would leave the receipt head behind the ledger and the state for every setup
// block — heights recovery takes the minimum of, so a run interrupted during setup over an existing
// directory would roll state back to a height the ledger has passed and then refuse to reopen.
func (w *receiptWriter) writeBlock(number int64, records []receipt.ReceiptRecord, encodedBytes int64) error {
	if err := w.store.SetReceipts(sdk.NewContext(nil, tmproto.Header{Height: number}, false), records); err != nil {
		return fmt.Errorf("failed to write the receipts for block %d: %w", number, err)
	}
	w.metrics.ReportReceiptsWritten(int64(len(records)))
	w.metrics.ReportStoreBytesWritten(storeReceiptDB, encodedBytes)
	return nil
}
