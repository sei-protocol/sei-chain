package main

import (
	"context"
	"os"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestLoadTestOtelMetricsPrometheusScrape(t *testing.T) {
	shutdown, err := initLoadtestOtelMetrics()
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	const bankMessageType = "bank"
	incrProducerEventCount(bankMessageType)
	incrConsumerEventCount(bankMessageType)
	setThroughputMetricByType("tps", 2.5, bankMessageType)

	mfs, err := loadtestPrometheusGatherer().Gather()
	require.NoError(t, err)

	host, err := os.Hostname()
	require.NoError(t, err)

	prod := findMetricFamily(mfs, "sei_loadtest_produce_count")
	require.NotNil(t, prod, "missing sei_loadtest_produce_count")
	require.Len(t, prod.Metric, 1)
	assertLoadtestLabels(t, prod.Metric[0], bankMessageType, host)
	require.Equal(t, float64(1), prod.Metric[0].GetCounter().GetValue())

	cons := findMetricFamily(mfs, "sei_loadtest_consume_count")
	require.NotNil(t, cons, "missing sei_loadtest_consume_count")
	require.Len(t, cons.Metric, 1)
	assertLoadtestLabels(t, cons.Metric[0], bankMessageType, host)
	require.Equal(t, float64(1), cons.Metric[0].GetCounter().GetValue())

	tps := findMetricFamily(mfs, "sei_loadtest_tps_tps")
	require.NotNil(t, tps, "missing sei_loadtest_tps_tps")
	require.Len(t, tps.Metric, 1)
	assertLoadtestLabels(t, tps.Metric[0], bankMessageType, host)
	require.Equal(t, 2.5, tps.Metric[0].GetGauge().GetValue())
}

func findMetricFamily(mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func assertLoadtestLabels(t *testing.T, m *dto.Metric, wantMsgType, wantHost string) {
	t.Helper()
	labels := make(map[string]string)
	for _, lp := range m.Label {
		labels[lp.GetName()] = lp.GetValue()
	}
	require.Equal(t, wantMsgType, labels["msg_type"])
	require.Equal(t, loadtestTelemetryServiceName, labels["service"])
	require.Equal(t, wantHost, labels["host"])
}
