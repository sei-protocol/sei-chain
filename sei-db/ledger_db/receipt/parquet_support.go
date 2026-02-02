package receipt

// ParquetEnabled returns whether parquet/DuckDB support is compiled in.
// This is a stub that always returns false. The parquet backend requires
// the duckdb build tag.
func ParquetEnabled() bool {
	return false
}
