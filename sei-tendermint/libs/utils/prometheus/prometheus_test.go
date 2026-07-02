package prometheus

import (
	"testing"

	stdprometheus "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestGaugeInt(t *testing.T) {
	desc := stdprometheus.NewDesc("test_gauge_int", "help", nil, nil)
	g := &GaugeInt{
		desc:       desc,
		labelPairs: stdprometheus.MakeLabelPairs(desc, nil),
	}

	g.Set(10)
	g.Add(5)
	g.Add(-3)

	metric := &dto.Metric{}
	require.NoError(t, g.Write(metric))
	require.NotNil(t, metric.Gauge)
	require.Equal(t, float64(12), metric.GetGauge().GetValue())
}

func TestCounterInt(t *testing.T) {
	desc := stdprometheus.NewDesc("test_counter_int", "help", nil, nil)
	c := &CounterInt{
		desc:       desc,
		labelPairs: stdprometheus.MakeLabelPairs(desc, nil),
	}

	c.Add(5)

	metric := &dto.Metric{}
	require.NoError(t, c.Write(metric))
	require.NotNil(t, metric.Counter)
	require.Equal(t, float64(5), metric.GetCounter().GetValue())
}

func TestCounterIntRejectsNegative(t *testing.T) {
	desc := stdprometheus.NewDesc("test_counter_int_negative", "help", nil, nil)
	c := &CounterInt{
		desc:       desc,
		labelPairs: stdprometheus.MakeLabelPairs(desc, nil),
	}

	require.Panics(t, func() {
		c.Add(-1)
	})
}

func TestGaugeIntVec(t *testing.T) {
	vec := NewGaugeIntVec(stdprometheus.GaugeOpts{
		Name: "test_gauge_int_vec",
		Help: "help",
	}, []string{"peer"})

	g := vec.WithLabelValues("p1")
	g.Add(7)

	dtoMetric := &dto.Metric{}
	require.NoError(t, g.Write(dtoMetric))
	require.Equal(t, float64(7), dtoMetric.GetGauge().GetValue())
	require.Len(t, dtoMetric.Label, 1)
	require.Equal(t, "peer", dtoMetric.Label[0].GetName())
	require.Equal(t, "p1", dtoMetric.Label[0].GetValue())
}

func TestCounterIntVec(t *testing.T) {
	vec := NewCounterIntVec(stdprometheus.CounterOpts{
		Name: "test_counter_int_vec",
		Help: "help",
	}, []string{"peer", "direction"})

	counter := vec.WithLabelValues("p1", "in")
	counter.Add(3)

	dtoMetric := &dto.Metric{}
	require.NoError(t, counter.Write(dtoMetric))
	require.Equal(t, float64(3), dtoMetric.GetCounter().GetValue())
	require.Len(t, dtoMetric.Label, 2)
	require.Equal(t, "peer", dtoMetric.Label[0].GetName())
	require.Equal(t, "p1", dtoMetric.Label[0].GetValue())
	require.Equal(t, "direction", dtoMetric.Label[1].GetName())
	require.Equal(t, "in", dtoMetric.Label[1].GetValue())
}
