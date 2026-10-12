package metrics

import (
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func sampleCount(t *testing.T, h *prometheus.Histogram) uint64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, h.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestObserveWaitLatency(t *testing.T) {
	m := Get()
	capacity := sampleCount(t, m.LaneCapacityWait)
	laneQCs := sampleCount(t, m.LaneQCWait)
	m.LaneCapacityWait.Observe((time.Millisecond).Seconds())
	m.LaneCapacityWait.Observe((2 * time.Millisecond).Seconds())
	m.LaneQCWait.Observe((time.Second).Seconds())
	require.Equal(t, capacity+2, sampleCount(t, m.LaneCapacityWait))
	require.Equal(t, laneQCs+1, sampleCount(t, m.LaneQCWait))
}

func TestObserveLaneCounters(t *testing.T) {
	m := Get()
	qcs := counterValue(t, m.LaneQCs)
	votes := counterValue(t, m.LaneVotesIngested)
	m.LaneQCs.Add(1)
	m.LaneVotesIngested.Add(1)
	m.LaneVotesIngested.Add(1)
	require.Equal(t, qcs+1, counterValue(t, m.LaneQCs))
	require.Equal(t, votes+2, counterValue(t, m.LaneVotesIngested))
}

func counterValue(t *testing.T, c *prometheus.CounterInt) int64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return int64(m.GetCounter().GetValue())
}

func TestEnterWait(t *testing.T) {
	g := Get().LaneCapacityInFlight
	var metric dto.Metric
	require.NoError(t, g.Write(&metric))
	before := metric.GetGauge().GetValue()
	g.Add(1)
	require.NoError(t, g.Write(&metric))
	require.Equal(t, before+1, metric.GetGauge().GetValue())
	g.Add(-1)
	require.NoError(t, g.Write(&metric))
	require.Equal(t, before, metric.GetGauge().GetValue())
}
