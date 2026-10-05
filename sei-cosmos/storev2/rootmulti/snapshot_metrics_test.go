package rootmulti

import (
	"bytes"
	"context"
	"testing"

	protoio "github.com/gogo/protobuf/io"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collectTotalNumKeys collects once from reader and returns the
// iavl_total_num_keys value of every store_name data point.
func collectTotalNumKeys(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))

	values := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "iavl_total_num_keys" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, point := range gauge.DataPoints {
				name, ok := point.Attributes.Value("store_name")
				require.True(t, ok, "data point has no store_name attribute")
				values[name.AsString()] = point.Value
			}
		}
	}
	return values
}

// TestSnapshotReportsZeroKeysForEmptyStore pins that a store exported with no
// nodes is reported with zero keys rather than left unreported. After the EVM
// migration the memiavl evm store is exported empty; a gauge that is never
// re-recorded keeps exporting its pre-migration value.
func TestSnapshotReportsZeroKeysForEmptyStore(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	cfg := evmMigratedConfig()
	evmData := newEVMTestData(0x56)
	store, storeKeys := newTestRootMulti(t, t.TempDir(), cfg)
	for block := 1; block <= 3; block++ {
		simulateBlock(t, store, storeKeys, block, evmData)
	}

	var buf bytes.Buffer
	require.NoError(t, store.Snapshot(3, protoio.NewDelimitedWriter(&buf)))
	require.NoError(t, store.Close())

	totals := collectTotalNumKeys(t, reader)
	require.Contains(t, totals, "evm", "empty memiavl evm store must still be reported")
	require.Zero(t, totals["evm"])
	require.Positive(t, totals["flatkv"], "migrated evm keys are reported under flatkv")
}
