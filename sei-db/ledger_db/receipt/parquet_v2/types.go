package parquet_v2

import "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2/coordinator"

type (
	ReplayResult        = coordinator.ReplayResult
	ReplayReceipt       = coordinator.ReplayReceipt
	WALReceiptConverter = coordinator.WALReceiptConverter
	ReplayHooks         = coordinator.ReplayHooks
)

var ErrStoreClosed = coordinator.ErrStoreClosed
