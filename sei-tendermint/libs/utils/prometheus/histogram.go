package prometheus

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// WARNING: there is no "DEFAULT" buckets. Empty list of buckets is a valid list
// and it results in having single +inf bucket.
type HistogramOpts = prometheus.HistogramOpts

type HistogramVec struct { v *prometheus.MetricVec }

func NewHistogramVec(opts HistogramOpts, labels []string) HistogramVec {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		labels,
		opts.ConstLabels,
	)
	return HistogramVec{
		v: prometheus.NewMetricVec(desc, func(lvs ...string) prometheus.Metric {
			return newHistogram(desc, opts, lvs...)
		}),
	}
}

func (h HistogramVec) WithLabelValues(values ...string) *Histogram {
	metric, err := h.v.GetMetricWithLabelValues(values...)
	if err != nil {
		panic(err)
	}
	return metric.(*Histogram)
}

// Histogram is equivalent to the standard histogram, except it additionally supports
// efficient ObserveWithWeight call.
type Histogram struct {
	desc                *prometheus.Desc
	variableLabelValues []string
	upperBounds         []float64 // exclusive of +Inf

	lock      sync.Mutex
	buckets   []uint64
	sum       float64
	createdAt time.Time
}

var _ prometheus.Metric = (*Histogram)(nil)
var _ prometheus.Collector = HistogramVec{}

func newHistogram(desc *prometheus.Desc, opts HistogramOpts, variableLabelValues ...string) *Histogram {
	for i, upperBound := range opts.Buckets {
		if i < len(opts.Buckets)-1 {
			if upperBound >= opts.Buckets[i+1] {
				panic(
					errors.New(
						"histogram buckets must be in increasing order",
					),
				)
			}
		} else if math.IsInf(upperBound, 1) {
			// The +Inf bucket is implicit in the export format.
			opts.Buckets = opts.Buckets[:i]
		}
	}

	upperBounds := make([]float64, len(opts.Buckets))
	copy(upperBounds, opts.Buckets)

	return &Histogram{
		desc:                desc,
		variableLabelValues: variableLabelValues,
		upperBounds:         upperBounds,
		buckets:             make([]uint64, len(upperBounds)+1),
		createdAt:           time.Now(),
	}
}

func (h *Histogram) Observe(value float64) {
	h.ObserveWithWeight(value, 1)
}

func (h *Histogram) ObserveWithWeight(value float64, weight uint64) {
	idx := sort.SearchFloat64s(h.upperBounds, value)
	h.lock.Lock()
	defer h.lock.Unlock()

	h.buckets[idx] += weight
	h.sum += value * float64(weight)
}

func (h *Histogram) Desc() *prometheus.Desc { return h.desc }

func (h *Histogram) Write(dest *dto.Metric) error {
	count, sum, buckets, createdAt := func() (uint64, float64, map[float64]uint64, time.Time) {
		h.lock.Lock()
		defer h.lock.Unlock()

		nBounds := len(h.upperBounds)
		buckets := make(map[float64]uint64, nBounds)
		var count uint64
		for idx, upperBound := range h.upperBounds {
			count += h.buckets[idx]
			buckets[upperBound] = count
		}
		count += h.buckets[nBounds]
		return count, h.sum, buckets, h.createdAt
	}()

	metric, err := prometheus.NewConstHistogramWithCreatedTimestamp(
		h.desc,
		count,
		sum,
		buckets,
		createdAt,
		h.variableLabelValues...,
	)
	if err != nil {
		return err
	}
	return metric.Write(dest)
}

func (h HistogramVec) Describe(ch chan<- *prometheus.Desc) { h.v.Describe(ch) }
func (h HistogramVec) Collect(ch chan<- prometheus.Metric) { h.v.Collect(ch) }
