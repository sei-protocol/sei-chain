package wal

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// The name of the OpenTelemetry meter for flatKV WAL metrics.
const walMeterName = "seidb_flatkv_wal"

var (
	walMeter = otel.Meter(walMeterName)

	// The number of blocks (end-of-block markers) written to the WAL.
	walBlocksWritten = must(walMeter.Int64Counter(
		"flatkv_wal_blocks_written",
		metric.WithDescription("Number of blocks written to the flatKV WAL"),
		metric.WithUnit("{count}"),
	))

	// The number of record bytes appended to the WAL (including framing).
	walBytesWritten = must(walMeter.Int64Counter(
		"flatkv_wal_bytes_written",
		metric.WithDescription("Number of bytes written to the flatKV WAL"),
		metric.WithUnit("By"),
	))

	// The number of WAL files sealed (rotated) after reaching the target size.
	walFilesSealed = must(walMeter.Int64Counter(
		"flatkv_wal_files_sealed",
		metric.WithDescription("Number of flatKV WAL files sealed on rotation"),
		metric.WithUnit("{count}"),
	))

	// The number of sealed WAL files deleted by pruning.
	walFilesPruned = must(walMeter.Int64Counter(
		"flatkv_wal_files_pruned",
		metric.WithDescription("Number of flatKV WAL files removed by pruning"),
		metric.WithUnit("{count}"),
	))
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
