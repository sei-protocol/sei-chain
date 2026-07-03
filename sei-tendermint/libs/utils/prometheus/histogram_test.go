package prometheus

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestHistogramNonMonotonicBuckets(t *testing.T) {
	testCases := map[string][]float64{
		"not strictly monotonic":  {1, 2, 2, 3},
		"not monotonic at all":    {1, 2, 4, 3, 5},
		"have +Inf in the middle": {1, 2, math.Inf(1), 3},
	}

	for name, buckets := range testCases {
		t.Run(name, func(t *testing.T) {
			require.Panics(t, func() {
				NewHistogramVec(
					HistogramOpts{
						Name:    "test_histogram",
						Help:    "helpless",
						Buckets: buckets,
					},
					nil,
				).WithLabelValues()
			})
		})
	}
}

func TestHistogramObserveWithWeight(t *testing.T) {
	vec := NewHistogramVec(
		HistogramOpts{
			Name:    "test_histogram",
			Help:    "helpless",
			Buckets: []float64{1, 2, 5},
		},
		[]string{"peer"},
	)

	histogram := vec.WithLabelValues("p1")
	histogram.ObserveWithWeight(0.5, 2)
	histogram.ObserveWithWeight(1.0, 3)
	histogram.ObserveWithWeight(1.5, 4)
	histogram.ObserveWithWeight(7.0, 5)

	metric := writeHistogramMetric(t, histogram)
	require.Len(t, metric.GetLabel(), 1)
	require.Equal(t, "peer", metric.GetLabel()[0].GetName())
	require.Equal(t, "p1", metric.GetLabel()[0].GetValue())

	exported := metric.GetHistogram()
	require.NotNil(t, exported)
	require.Equal(t, uint64(14), exported.GetSampleCount())
	require.Equal(t, 45.0, exported.GetSampleSum())
	require.Len(t, exported.GetBucket(), 3)
	require.Equal(t, 1.0, exported.GetBucket()[0].GetUpperBound())
	require.Equal(t, uint64(5), exported.GetBucket()[0].GetCumulativeCount())
	require.Equal(t, 2.0, exported.GetBucket()[1].GetUpperBound())
	require.Equal(t, uint64(9), exported.GetBucket()[1].GetCumulativeCount())
	require.Equal(t, 5.0, exported.GetBucket()[2].GetUpperBound())
	require.Equal(t, uint64(9), exported.GetBucket()[2].GetCumulativeCount())
}

func TestHistogramWithoutBucketsExportsOnlyInfBucket(t *testing.T) {
	vec := NewHistogramVec(
		HistogramOpts{
			Name: "test_no_buckets_histogram",
			Help: "help",
		},
		nil,
	)
	histogram := vec.WithLabelValues()
	histogram.Observe(3)

	metric := writeHistogramMetric(t, histogram)
	require.NotNil(t, metric.GetHistogram())
	require.Equal(t, uint64(1), metric.GetHistogram().GetSampleCount())
	require.Len(t, metric.GetHistogram().GetBucket(), 0)

	name := "test_no_buckets_histogram"
	help := "help"
	mf := &dto.MetricFamily{
		Name:   &name,
		Help:   &help,
		Type:   dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{metric},
	}

	var out bytes.Buffer
	_, err := expfmt.MetricFamilyToText(&out, mf)
	require.NoError(t, err)
	text := out.String()
	require.Contains(t, text, "test_no_buckets_histogram_bucket{le=\"+Inf\"} 1")
	require.Equal(t, 1, strings.Count(text, "test_no_buckets_histogram_bucket{"))
	require.Contains(t, text, "test_no_buckets_histogram_sum 3")
	require.Contains(t, text, "test_no_buckets_histogram_count 1")
}

func TestHistogramCreatedTimestamp(t *testing.T) {
	before := time.Now()
	vec := NewHistogramVec(
		HistogramOpts{
			Name: "test_histogram_created",
			Help: "helpless",
		},
		[]string{"peer"},
	)
	histogram := vec.WithLabelValues("p1")
	afterCreate := time.Now()

	metric := writeHistogramMetric(t, histogram)
	createdAt := metric.GetHistogram().GetCreatedTimestamp()
	require.NotNil(t, createdAt)
	require.False(t, createdAt.AsTime().Before(before))
	require.False(t, createdAt.AsTime().After(afterCreate))
}

func TestHistogramVecCreatedTimestampWithDeletes(t *testing.T) {
	vec := NewHistogramVec(
		HistogramOpts{
			Name: "test_histogram_delete_recreate",
			Help: "helpless",
		},
		[]string{"peer"},
	)

	firstMetric := writeHistogramMetric(t, vec.WithLabelValues("p1"))
	firstCreatedAt := firstMetric.GetHistogram().GetCreatedTimestamp()
	require.NotNil(t, firstCreatedAt)

	require.True(t, vec.v.DeleteLabelValues("p1"))
	afterDelete := time.Now()

	secondMetric := writeHistogramMetric(t, vec.WithLabelValues("p1"))
	secondCreatedAt := secondMetric.GetHistogram().GetCreatedTimestamp()
	require.NotNil(t, secondCreatedAt)
	require.False(t, secondCreatedAt.AsTime().Before(afterDelete))
}

func writeHistogramMetric(t *testing.T, histogram *Histogram) *dto.Metric {
	t.Helper()

	metric := &dto.Metric{}
	require.NoError(t, histogram.Write(metric))
	return metric
}
