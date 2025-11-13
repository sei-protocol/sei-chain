package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	emitMetrics()

	gr, err := m.Gather(FormatText)
	require.NoError(t, err)
	require.Equal(t, gr.ContentType, "application/json")

	jsonMetrics := make(map[string]interface{})
	require.NoError(t, json.Unmarshal(gr.Metrics, &jsonMetrics))

	counters := jsonMetrics["Counters"].([]interface{})
	// With 100ms ticker and 1s timeout, we get ~10 increments (1s / 100ms)
	// Allow some variance due to timing
	require.GreaterOrEqual(t, counters[0].(map[string]interface{})["Count"].(float64), 3.0)
	require.Equal(t, counters[0].(map[string]interface{})["Name"].(string), "test.dummy_counter")
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

	emitMetrics()

	gr, err := m.Gather(FormatPrometheus)
	require.NoError(t, err)
	require.Equal(t, gr.ContentType, string(expfmt.FmtText))

	// With 100ms ticker and 1s timeout, we get ~10 increments (1s / 100ms)
	require.True(t, strings.Contains(string(gr.Metrics), "test_dummy_counter"))
}

func emitMetrics() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(1 * time.Second)

	for {
		select {
		case <-ticker.C:
			metrics.IncrCounter([]string{"dummy_counter"}, 1.0)
		case <-timeout:
			return
		}
	}
}
