package parquet_v2

import "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2/coordinator"

type (
	ReplayResult        = coordinator.ReplayResult
	ReplayReceipt       = coordinator.ReplayReceipt
	ReplayedBlock       = coordinator.ReplayedBlock
	WALReceiptConverter = coordinator.WALReceiptConverter
)

var ErrStoreClosed = coordinator.ErrStoreClosed
