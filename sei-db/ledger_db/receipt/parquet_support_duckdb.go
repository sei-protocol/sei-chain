//go:build duckdb
// +build duckdb

package receipt

import "github.com/sei-protocol/sei-chain/sei-db/ledger_db/parquet"

// ParquetEnabled returns whether parquet/DuckDB support is compiled in.
func ParquetEnabled() bool {
	return parquet.Enabled()
}
