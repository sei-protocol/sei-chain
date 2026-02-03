//go:build !duckdb
// +build !duckdb

package receipt

import "testing"

// benchmarkParquetWriteAsync is a stub for non-duckdb builds.
func benchmarkParquetWriteAsync(b *testing.B, receiptsPerBlock int, blocks int) {
	b.Skip("parquet benchmarks require -tags duckdb")
}

// benchmarkParquetWriteNoWAL is a stub for non-duckdb builds.
func benchmarkParquetWriteNoWAL(b *testing.B, receiptsPerBlock int, blocks int) {
	b.Skip("parquet benchmarks require -tags duckdb")
}
