package composite

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

const (
	meterName                    = "seidb_sc_composite"
	writeModeInfoInstrument      = "seidb_sc_write_mode_info"
	migrationBatchSizeInstrument = "seidb_migration_batch_size"
)

// registerStoreTelemetry makes meter report the effective write mode and the migration batch size of cs at
// each collection, until the returned registration is unregistered. Nothing is reported before a load has
// resolved the effective write mode.
func registerStoreTelemetry(meter metric.Meter, cs *CompositeCommitStore) (metric.Registration, error) {
	writeModeInfo, err := meter.Int64ObservableGauge(
		writeModeInfoInstrument,
		metric.WithDescription("Effective state commit write mode. Value is always 1; inspect the mode label."),
		metric.WithUnit("{mode}"),
	)
	if err != nil {
		return nil, err
	}
	batchSize, err := meter.Int64ObservableGauge(
		migrationBatchSizeInstrument,
		metric.WithDescription("Governance-controlled number of keys to migrate per block. 0 means paused."),
		metric.WithUnit("{count}"),
	)
	if err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		mode := cs.loadWriteMode()
		if mode == "" || mode == types.Auto {
			return nil
		}
		observer.ObserveInt64(writeModeInfo, 1, metric.WithAttributes(attribute.String("mode", string(mode))))
		observer.ObserveInt64(batchSize, cs.migrationBatchSize.Load())
		return nil
	}, writeModeInfo, batchSize)
}
