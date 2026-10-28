package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRuntimeMetricsAreExported(t *testing.T) {
	require.NoError(t, SetupPrometheus())
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	joined := strings.Join(names, " ")
	require.Contains(t, joined, "go_sched_latencies_seconds")
	require.Contains(t, joined, "go_sync_mutex_wait_total_seconds_total")
	require.Contains(t, joined, "go_cpu_classes_idle_cpu_seconds_total")
}
