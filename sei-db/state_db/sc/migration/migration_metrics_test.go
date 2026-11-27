package migration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	boundarySnapshotInstrument = "seidb_migration_boundary_snapshot"
	boundaryInfoInstrument     = "seidb_migration_boundary_info"
)

// collectBoundaryLabels collects once from reader and returns the boundary_hex label of each
// boundary snapshot data point.
func collectBoundaryLabels(t *testing.T, reader *sdkmetric.ManualReader) []string {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))

	var labels []string
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != boundarySnapshotInstrument {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, point := range gauge.DataPoints {
				require.Equal(t, int64(1), point.Value)
				label, ok := point.Attributes.Value("boundary_hex")
				require.True(t, ok, "data point has no boundary_hex attribute")
				labels = append(labels, label.AsString())
			}
		}
	}
	return labels
}

func newTestMigrationMetrics(t *testing.T) (*MigrationMetrics, *sdkmetric.ManualReader) {
	t.Helper()
	return newTestMigrationMetricsTo(t, Version1_MigrateEVM)
}

// newTestMigrationMetricsTo returns metrics for the migration to targetVersion, at the version before it.
func newTestMigrationMetricsTo(t *testing.T, targetVersion uint64) (*MigrationMetrics, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	m := newMigrationMetrics(context.Background(), provider.Meter("test"), targetVersion)
	t.Cleanup(m.Close)
	m.SetVersion(targetVersion - 1)
	return m, reader
}

// collectBoundaryInfo collects once from reader and returns "phase/status/module" for each boundary info
// data point.
func collectBoundaryInfo(t *testing.T, reader *sdkmetric.ManualReader) []string {
	t.Helper()

	var infos []string
	for _, m := range collectMetrics(t, reader) {
		if m.Name != boundaryInfoInstrument {
			continue
		}
		gauge, ok := m.Data.(metricdata.Gauge[int64])
		require.True(t, ok, "unexpected data type %T", m.Data)
		for _, point := range gauge.DataPoints {
			require.Equal(t, int64(1), point.Value)
			phase, _ := point.Attributes.Value("migration_phase")
			status, _ := point.Attributes.Value("status")
			module, _ := point.Attributes.Value("module")
			infos = append(infos, phase.AsString()+"/"+status.AsString()+"/"+module.AsString())
		}
	}
	return infos
}

// collectPhases collects once from reader and returns, for each instrument, the migration_phase label of
// each data point. A data point without the label is reported as "".
func collectPhases(t *testing.T, reader *sdkmetric.ManualReader) map[string][]string {
	t.Helper()

	phases := make(map[string][]string)
	add := func(name string, attrs attribute.Set) {
		phase, _ := attrs.Value("migration_phase")
		phases[name] = append(phases[name], phase.AsString())
	}
	for _, m := range collectMetrics(t, reader) {
		switch data := m.Data.(type) {
		case metricdata.Sum[int64]:
			for _, point := range data.DataPoints {
				add(m.Name, point.Attributes)
			}
		case metricdata.Gauge[int64]:
			for _, point := range data.DataPoints {
				add(m.Name, point.Attributes)
			}
		case metricdata.Histogram[float64]:
			for _, point := range data.DataPoints {
				add(m.Name, point.Attributes)
			}
		default:
			t.Fatalf("unexpected data type %T for %s", m.Data, m.Name)
		}
	}
	return phases
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()

	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))
	var metrics []metricdata.Metrics
	for _, scope := range collected.ScopeMetrics {
		metrics = append(metrics, scope.Metrics...)
	}
	return metrics
}

func TestBoundarySnapshotReportsOnlyTheCurrentBoundary(t *testing.T) {
	m, reader := newTestMigrationMetrics(t)

	first := NewMigrationBoundary("evm", []byte{0x01})
	m.SetBoundary(first)
	require.Equal(t, []string{first.String()}, collectBoundaryLabels(t, reader))

	second := NewMigrationBoundary("evm", []byte{0x02})
	m.SetBoundary(second)
	require.Equal(t, []string{second.String()}, collectBoundaryLabels(t, reader))
}

func TestBoundarySnapshotReportsCompleteAtTargetVersion(t *testing.T) {
	m, reader := newTestMigrationMetrics(t)

	m.SetBoundary(NewMigrationBoundary("evm", []byte{0x01}))
	m.SetVersion(Version1_MigrateEVM)
	require.Equal(t, []string{"complete"}, collectBoundaryLabels(t, reader))
}

func TestBoundarySnapshotStopsAfterClose(t *testing.T) {
	m, reader := newTestMigrationMetrics(t)

	m.SetBoundary(NewMigrationBoundary("evm", []byte{0x01}))
	require.Len(t, collectBoundaryLabels(t, reader), 1)

	m.Close()
	require.Empty(t, collectBoundaryLabels(t, reader))
	require.Empty(t, collectBoundaryInfo(t, reader))
}

func TestBoundaryInfoReportsStatusAndModule(t *testing.T) {
	m, reader := newTestMigrationMetricsTo(t, Version2_FlatKVOnly)

	m.SetBoundary(MigrationBoundaryNotStarted)
	require.Equal(t, []string{"cosmos/not_started/"}, collectBoundaryInfo(t, reader))

	m.SetBoundary(NewMigrationBoundary("bank", []byte{0x02}))
	require.Equal(t, []string{"cosmos/in_progress/bank"}, collectBoundaryInfo(t, reader))

	m.SetBoundary(NewMigrationBoundary("staking", []byte{0x21}))
	require.Equal(t, []string{"cosmos/in_progress/staking"}, collectBoundaryInfo(t, reader))

	m.SetVersion(Version2_FlatKVOnly)
	require.Equal(t, []string{"cosmos/complete/"}, collectBoundaryInfo(t, reader))
}

func TestMigrationInstrumentsReportTheirPhase(t *testing.T) {
	for _, tc := range []struct {
		targetVersion uint64
		phase         string
	}{
		{targetVersion: Version1_MigrateEVM, phase: "evm"},
		{targetVersion: Version2_FlatKVOnly, phase: "cosmos"},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			m, reader := newTestMigrationMetricsTo(t, tc.targetVersion)
			m.SetBoundary(NewMigrationBoundary("bank", []byte{0x02}))
			m.RecordBatch(migrationBatchStats{
				keysMigrated:             1,
				keyBytesMigrated:         1,
				valueBytesMigrated:       1,
				originalPairsRoutedOldDB: 1,
				originalPairsRoutedNewDB: 1,
				oldDBPairsWritten:        1,
				newDBPairsWritten:        1,
			})
			m.RecordApplyDuration(time.Millisecond)

			phases := collectPhases(t, reader)
			require.Equal(t, []string{""}, phases[boundarySnapshotInstrument])
			delete(phases, boundarySnapshotInstrument)
			require.Len(t, phases, 11)
			for name, got := range phases {
				require.Equal(t, []string{tc.phase}, got, name)
			}
		})
	}
}
