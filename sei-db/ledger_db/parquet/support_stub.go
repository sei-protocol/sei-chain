//go:build !duckdb
// +build !duckdb

package parquet

// Enabled returns whether parquet/DuckDB support is compiled in.
func Enabled() bool {
	return false
}
