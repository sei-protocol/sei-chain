package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const boundarySnapshotInstrument = "seidb_migration_boundary_snapshot"

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

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	m := newMigrationMetrics(context.Background(), provider.Meter("test"), Version1_MigrateEVM)
	t.Cleanup(m.Close)
	m.SetVersion(Version0_MemiavlOnly)
	return m, reader
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
}
