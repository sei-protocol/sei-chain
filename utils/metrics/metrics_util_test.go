package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestSetupOtelMetricsProviderRequiresChainID(t *testing.T) {
	require.Error(t, SetupOtelMetricsProvider(""))
}

// TestSetupOtelMetricsProviderAttachesChainID verifies that chain_id is emitted
// as a constant label on every OTel metric series scraped via Prometheus
func TestSetupOtelMetricsProviderAttachesChainID(t *testing.T) {
	const chainID = "test-chain-1"
	require.NoError(t, SetupOtelMetricsProvider(chainID))

	// Emit a measurement through the global provider the setup just installed.
	counter, err := otel.Meter("meter_test").Int64Counter("meter_chain_id_probe")
	require.NoError(t, err)
	counter.Add(t.Context(), 1)

	// Gathering triggers the exporter's pull-based collection.
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	var found bool
	for _, mf := range families {
		if !strings.Contains(mf.GetName(), "meter_chain_id_probe") {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "chain_id" {
					require.Equal(t, chainID, l.GetValue())
					found = true
				}
			}
		}
	}
	require.True(t, found, "expected chain_id label on the emitted OTel metric")
}
