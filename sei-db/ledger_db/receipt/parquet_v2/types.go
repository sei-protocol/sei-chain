package parquet_v2

import "github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt/parquet_v2/coordinator"

type (
	ReplayResult  = coordinator.ReplayResult
	ReplayedBlock = coordinator.ReplayedBlock
)

var ErrStoreClosed = coordinator.ErrStoreClosed
