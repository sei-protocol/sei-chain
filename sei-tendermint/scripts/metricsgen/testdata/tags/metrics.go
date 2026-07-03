package tags

import (
	"github.com/prometheus/client_golang/prometheus"
	tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const (
	MetricsNamespace = "tendermint"
	MetricsSubsystem = "tags"
)

//go:generate go run ../../../../scripts/metricsgen -struct=Metrics

type Metrics struct {
	WithLabels     *prometheus.CounterVec    `metrics_labels:"step,time"`
	WithExpBuckets tmprometheus.HistogramVec `metrics_buckets:"exp(.1,100,8)"`
	WithBuckets    tmprometheus.HistogramVec `metrics_buckets:"1, 2, 3, 4, 5"`
	WithNoBuckets  tmprometheus.HistogramVec `metrics_buckets:"none"`
	Named          *prometheus.CounterVec    `metrics_name:"metric_with_name"`
}
