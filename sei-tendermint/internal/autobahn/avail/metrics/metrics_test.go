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
	capacity := sampleCount(t, Global.laneCapacityWaitLatencyAt())
	laneQCs := sampleCount(t, Global.laneQcWaitLatencyAt())
	ObserveLaneCapacityWait(time.Millisecond)
	ObserveLaneCapacityWait(2 * time.Millisecond)
	ObserveLaneQCWait(time.Second)
	require.Equal(t, capacity+2, sampleCount(t, Global.laneCapacityWaitLatencyAt()))
	require.Equal(t, laneQCs+1, sampleCount(t, Global.laneQcWaitLatencyAt()))
}

func TestEnterWait(t *testing.T) {
	g := Global.inFlightAt(WaitLaneCapacity)
	var m dto.Metric
	require.NoError(t, g.Write(&m))
	before := m.GetGauge().GetValue()
	leave := EnterWait(WaitLaneCapacity)
	require.NoError(t, g.Write(&m))
	require.Equal(t, before+1, m.GetGauge().GetValue())
	leave()
	require.NoError(t, g.Write(&m))
	require.Equal(t, before, m.GetGauge().GetValue())
}
