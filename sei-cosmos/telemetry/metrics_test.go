package telemetry

import (
	"encoding/json"
	"strings"
	"testing"

	metrics "github.com/armon/go-metrics"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/require"
)

func TestMetrics_Disabled(t *testing.T) {
	m, err := New(Config{Enabled: false})
	require.Nil(t, m)
	require.Nil(t, err)
}

func TestMetrics_InMem(t *testing.T) {
	m, err := New(Config{
		Enabled:        true,
		EnableHostname: false,
		ServiceName:    "test",
	})
	require.NoError(t, err)
	require.NotNil(t, m)

	emitMetrics(10)

	gr, err := m.Gather(FormatText)
	require.NoError(t, err)
	require.Equal(t, gr.ContentType, "application/json")

	var jsonMetrics struct {
		Counters []struct {
			Name  string
			Count int
		}
	}
	require.NoError(t, json.Unmarshal(gr.Metrics, &jsonMetrics))

	require.Len(t, jsonMetrics.Counters, 1)
	require.Equal(t, "test.dummy_counter", jsonMetrics.Counters[0].Name)
	require.Equal(t, 10, jsonMetrics.Counters[0].Count)
}

func TestMetrics_Prom(t *testing.T) {
	m, err := New(Config{
		Enabled:                 true,
		EnableHostname:          false,
		ServiceName:             "test",
		PrometheusRetentionTime: 60,
		EnableHostnameLabel:     false,
	})
	require.NoError(t, err)
	require.NotNil(t, m)
	require.True(t, m.prometheusEnabled)

	emitMetrics(30)

	gr, err := m.Gather(FormatPrometheus)
	require.NoError(t, err)
	require.Equal(t, gr.ContentType, string(expfmt.NewFormat(expfmt.TypeTextPlain)))

	require.True(t, strings.Contains(string(gr.Metrics), "test_dummy_counter 30"))
}

func emitMetrics(count int) {
	for range count {
		metrics.IncrCounter([]string{"dummy_counter"}, 1.0)
	}
}
