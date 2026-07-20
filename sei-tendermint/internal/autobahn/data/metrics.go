package data

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/component-base/metrics/prometheusextension"
)

var _ prometheus.Collector = (*State)(nil)

type latencyMetric struct {
	*prometheusextension.WeightedHistogramVec
}

type resourceLatencyMetric struct {
	Receive prometheusextension.WeightedObserver
	Execute prometheusextension.WeightedObserver
	Prune   prometheusextension.WeightedObserver
}

func newLatencyMetric() latencyMetric {
	return latencyMetric{
		prometheusextension.NewWeightedHistogramVec(prometheus.HistogramOpts{
			Name:    "sei_data__latency",
			Help:    "latency of resource processing up from production to the given stage",
			Buckets: prometheus.ExponentialBuckets(0.001, 1.5, 30),
		}, "resource", "stage"),
	}
}

func (m latencyMetric) resource(resource string) resourceLatencyMetric {
	return resourceLatencyMetric{
		Receive: m.WithLabelValues(resource, "receive"),
		Execute: m.WithLabelValues(resource, "execute"),
		Prune:   m.WithLabelValues(resource, "prune"),
	}
}

type dataMetrics struct {
	Base   latencyMetric
	Blocks resourceLatencyMetric
	Txs    resourceLatencyMetric
}

func newDataMetrics() *dataMetrics {
	base := newLatencyMetric()
	return &dataMetrics{
		Base:   base,
		Blocks: base.resource("blocks"),
		Txs:    base.resource("txs"),
	}
}

// Describe from prometheus.Collector.
func (s *State) Describe(chan<- *prometheus.Desc) {}

// Collect from prometheus.Collector.
func (s *State) Collect(m chan<- prometheus.Metric) {
	s.metrics.Base.Collect(m)
}
