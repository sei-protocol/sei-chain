package types

import (
	"github.com/go-kit/kit/metrics/discard"
	prometheus "github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

func PrometheusProxyMetrics(namespace string, labelsAndValues ...string) *ProxyMetrics {
	labels := []string{}
	for i := range len(labelsAndValues) / 2 {
		labels = append(labels, labelsAndValues[i*2])
	}
	return &ProxyMetrics{
		MethodTiming: prometheus.NewHistogramFrom(stdprometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: ProxyMetricsSubsystem,
			Name:      "method_timing",
			Help:      "Timing for each ABCI method.",
			Buckets:   []float64{.0001, .0004, .002, .009, .02, .1, .65, 2, 6, 25},
		}, append(labels, "method", "type")).With(labelsAndValues...),
	}
}

func NopProxyMetrics() *ProxyMetrics {
	return &ProxyMetrics{
		MethodTiming: discard.NewHistogram(),
	}
}
