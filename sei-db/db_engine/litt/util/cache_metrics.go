package util

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CacheMetrics is a struct that holds metrics for a cache. A nil CacheMetrics instance acts as a no-op.
type CacheMetrics struct {
	keyCount        *prometheus.GaugeVec
	weight          *prometheus.GaugeVec
	keysAdded       *prometheus.CounterVec
	weightAdded     *prometheus.CounterVec
	evictionLatency *prometheus.SummaryVec
}

// NewCacheMetrics creates a new CacheMetrics instance. If the registry is nil, it returns nil.
// The cacheName does not need to include the suffix "_cache" as this is added automatically.
func NewCacheMetrics(registry *prometheus.Registry, namespace string, cacheName string) *CacheMetrics {
	if registry == nil {
		return nil
	}

	evictionLatency := promauto.With(registry).NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      cacheName + "_cache_eviction_latency_ms",
			Help:      "Reports on the eviction latency of the cache.",
		},
		[]string{},
	)

	keyCount := promauto.With(registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      cacheName + "_cache_key_count",
			Help:      "Reports on the number of keys in the cache",
		},
		[]string{},
	)

	weight := promauto.With(registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      cacheName + "_cache_weight",
			Help:      "Reports on the weight of the cache",
		},
		[]string{},
	)

	keysAdded := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      cacheName + "_cache_keys_added",
			Help:      "Reports on the number of keys added to the cache",
		},
		[]string{},
	)

	weightAdded := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      cacheName + "_cache_weight_added",
			Help:      "Reports on the weight of the entries added to the cache",
		},
		[]string{},
	)

	return &CacheMetrics{
		keyCount:        keyCount,
		weight:          weight,
		keysAdded:       keysAdded,
		weightAdded:     weightAdded,
		evictionLatency: evictionLatency,
	}
}

// reportInsertion is used to report an entry being inserted into the cache.
func (m *CacheMetrics) reportInsertion(weight uint64) {
	if m == nil {
		return
	}

	m.keysAdded.WithLabelValues().Inc()
	m.weightAdded.WithLabelValues().Add(float64(weight))
}

// reportEviction is used to report an entry being evicted from the cache.
func (m *CacheMetrics) reportEviction(age time.Duration) {
	if m == nil {
		return
	}

	m.evictionLatency.WithLabelValues().Observe(ToMilliseconds(age))
}

// reportCurrentSize is used to report the current size/weight of the cache.
func (m *CacheMetrics) reportCurrentSize(size int, weight uint64) {
	if m == nil {
		return
	}

	m.keyCount.WithLabelValues().Set(float64(size))
	m.weight.WithLabelValues().Set(float64(weight))
}
