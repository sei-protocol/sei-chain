//go:build !duckdb
// +build !duckdb

package receipt

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dbLogger "github.com/sei-protocol/sei-chain/sei-db/common/logger"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"
)

// ParquetEnabled returns whether parquet/DuckDB support is compiled in.
func ParquetEnabled() bool {
	return parquet.Enabled()
}

// newParquetReceiptStore is a stub that returns an error when duckdb tag is not set.
func newParquetReceiptStore(_ dbLogger.Logger, _ dbconfig.ReceiptStoreConfig, _ sdk.StoreKey) (ReceiptStore, error) {
	return nil, errors.New("parquet receipt store requires duckdb build tag")
}
