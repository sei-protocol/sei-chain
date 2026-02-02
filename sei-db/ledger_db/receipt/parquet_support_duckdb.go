//go:build duckdb
// +build duckdb

package receipt

// ParquetEnabled returns whether parquet/DuckDB support is compiled in.
func ParquetEnabled() bool {
	return true
}
