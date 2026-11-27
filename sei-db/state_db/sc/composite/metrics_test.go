package composite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// collectStoreTelemetry collects once from reader and returns the mode label of each write mode data point
// and the value of each migration batch size data point.
func collectStoreTelemetry(t *testing.T, reader *sdkmetric.ManualReader) ([]string, []int64) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))

	var modes []string
	var batchSizes []int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != writeModeInfoInstrument && m.Name != migrationBatchSizeInstrument {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, point := range gauge.DataPoints {
				if m.Name == migrationBatchSizeInstrument {
					batchSizes = append(batchSizes, point.Value)
					continue
				}
				require.Equal(t, int64(1), point.Value)
				mode, ok := point.Attributes.Value("mode")
				require.True(t, ok, "data point has no mode attribute")
				modes = append(modes, mode.AsString())
			}
		}
	}
	return modes, batchSizes
}

func requireStoreTelemetry(t *testing.T, reader *sdkmetric.ManualReader, mode types.WriteMode, batchSize int64) {
	t.Helper()
	modes, batchSizes := collectStoreTelemetry(t, reader)
	require.Equal(t, []string{string(mode)}, modes)
	require.Equal(t, []int64{batchSize}, batchSizes)
}

func TestStoreTelemetryReportsTheLiveWriteModeAndBatchSize(t *testing.T) {
	reader := installManualMeterProvider(t)
	dir := t.TempDir()
	workload := newMigrationWorkload(0x7e1e)

	cs, err := NewCompositeCommitStore(t.Context(), dir, autoConfig())
	require.NoError(t, err)
	modes, batchSizes := collectStoreTelemetry(t, reader)
	require.Empty(t, modes, "an unresolved Auto mode must not be reported")
	require.Empty(t, batchSizes)

	require.NoError(t, cs.SetMigrationBatchSize(25))
	require.NoError(t, cs.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	require.NoError(t, cs.LoadLatest())
	requireStoreTelemetry(t, reader, types.MemiavlOnly, 25)

	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	requireStoreTelemetry(t, reader, types.MigrateEVM, 25)
	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 200)
	require.NoError(t, cs.SetWriteMode(types.EVMMigrated))
	require.NoError(t, cs.SetMigrationBatchSize(0))
	requireStoreTelemetry(t, reader, types.EVMMigrated, 0)

	require.NoError(t, cs.SetMigrationBatchSize(1024))
	require.NoError(t, cs.SetWriteMode(types.MigrateCosmos))
	requireStoreTelemetry(t, reader, types.MigrateCosmos, 1024)

	ro, err := cs.LoadVersionReadOnly(0)
	require.NoError(t, err)
	requireStoreTelemetry(t, reader, types.MigrateCosmos, 1024)
	require.NoError(t, ro.Close())

	runUntilAtMigrationVersion(t, cs, workload, migration.Version2_FlatKVOnly, 200)
	require.NoError(t, cs.SetWriteMode(types.FlatKVOnly))
	requireStoreTelemetry(t, reader, types.FlatKVOnly, 1024)

	require.NoError(t, cs.Close())
	modes, batchSizes = collectStoreTelemetry(t, reader)
	require.Empty(t, modes, "a closed store must not be reported")
	require.Empty(t, batchSizes)
}

func TestStoreTelemetryReportsTheResolvedModeAfterRestart(t *testing.T) {
	reader := installManualMeterProvider(t)
	dir := t.TempDir()
	workload := newMigrationWorkload(0x7e1f)

	cs := openAutoStore(t, dir, 25)
	require.NoError(t, cs.SetWriteMode(types.MigrateEVM))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version1_MigrateEVM, 200)
	require.NoError(t, cs.SetWriteMode(types.EVMMigrated))
	require.NoError(t, cs.SetWriteMode(types.MigrateCosmos))
	runUntilAtMigrationVersion(t, cs, workload, migration.Version2_FlatKVOnly, 200)
	require.NoError(t, cs.Close())

	reopened := openAutoStore(t, dir, 7)
	defer func() { _ = reopened.Close() }()
	requireStoreTelemetry(t, reader, types.FlatKVOnly, 7)
}
