package types

import abci "github.com/tendermint/tendermint/abci/types"

// DeliverTxEntry represents an individual transaction's request within a batch.
// This can be extended to include tx-level tracing or metadata
type DeliverTxEntry struct {
	Request abci.RequestDeliverTx
}

// DeliverTxBatchRequest represents a request object for a batch of transactions.
// This can be extended to include request-level tracing or metadata
type DeliverTxBatchRequest struct {
	TxEntries []*DeliverTxEntry
}

// DeliverTxResult represents an individual transaction's response within a batch.
// This can be extended to include tx-level tracing or metadata
type DeliverTxResult struct {
	Response abci.ResponseDeliverTx
}

// DeliverTxBatchResponse represents a response object for a batch of transactions.
// This can be extended to include response-level tracing or metadata
type DeliverTxBatchResponse struct {
	Results []*DeliverTxResult
}
