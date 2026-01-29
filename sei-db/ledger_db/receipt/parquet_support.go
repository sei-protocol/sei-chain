package receipt

// ParquetEnabled reports whether the parquet/duckdb receipt backend is available
// in this build.
func ParquetEnabled() bool {
	return parquetEnabled
}
