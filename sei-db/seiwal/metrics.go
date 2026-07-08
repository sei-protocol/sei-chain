package seiwal

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// The name of the OpenTelemetry meter for WAL metrics.
const walMeterName = "seidb_seiwal"

var (
	walMeter = otel.Meter(walMeterName)

	// The number of records appended to the WAL.
	walRecordsWritten = must(walMeter.Int64Counter(
		"seiwal_records_written",
		metric.WithDescription("Number of records appended to the WAL"),
		metric.WithUnit("{count}"),
	))

	// The number of record bytes appended to the WAL (including framing).
	walBytesWritten = must(walMeter.Int64Counter(
		"seiwal_bytes_written",
		metric.WithDescription("Number of bytes written to the WAL"),
		metric.WithUnit("By"),
	))

	// The number of WAL files sealed (rotated) after reaching the target size.
	walFilesSealed = must(walMeter.Int64Counter(
		"seiwal_files_sealed",
		metric.WithDescription("Number of WAL files sealed on rotation"),
		metric.WithUnit("{count}"),
	))

	// The number of sealed WAL files deleted by pruning.
	walFilesPruned = must(walMeter.Int64Counter(
		"seiwal_files_pruned",
		metric.WithDescription("Number of WAL files removed by pruning"),
		metric.WithUnit("{count}"),
	))
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
