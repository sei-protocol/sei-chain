package prometheus

import (
	"errors"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var _ prometheus.Metric = (*GaugeInt)(nil)
var _ prometheus.Metric = (*CounterInt)(nil)
var _ prometheus.Collector = GaugeIntVec{}
var _ prometheus.Collector = CounterIntVec{}

// GaugeInt is a Metric that represents a single int64 value that can
// arbitrarily go up and down.
type GaugeInt struct {
	value      atomic.Int64
	desc       *prometheus.Desc
	labelPairs []*dto.LabelPair
}

func (g *GaugeInt) Set(val int64)          { g.value.Store(val) }
func (g *GaugeInt) Add(val int64)          { g.value.Add(val) }
func (g *GaugeInt) Desc() *prometheus.Desc { return g.desc }
func (g *GaugeInt) Write(out *dto.Metric) error {
	out.Label = g.labelPairs
	out.Gauge = &dto.Gauge{Value: proto.Float64(float64(g.value.Load()))}
	return nil
}

// CounterInt is a Metric that represents a single int64 value that only ever
// goes up.
type CounterInt struct {
	value      atomic.Int64
	desc       *prometheus.Desc
	labelPairs []*dto.LabelPair
}

func (c *CounterInt) Desc() *prometheus.Desc { return c.desc }
func (c *CounterInt) Add(val int64) {
	if val < 0 {
		panic(errors.New("counter cannot decrease in value"))
	}
	c.value.Add(val)
}
func (c *CounterInt) Write(out *dto.Metric) error {
	out.Label = c.labelPairs
	out.Counter = &dto.Counter{Value: proto.Float64(float64(c.value.Load()))}
	return nil
}

// GaugeIntVec is a Collector that bundles a set of GaugeInt metrics that all
// share the same Desc, but have different values for their variable labels.
type GaugeIntVec struct{ v *prometheus.MetricVec }

// CounterIntVec is a Collector that bundles a set of CounterInt metrics that
// all share the same Desc, but have different values for their variable labels.
type CounterIntVec struct{ v *prometheus.MetricVec }

// NewGaugeIntVec creates a new GaugeIntVec based on the provided GaugeOpts and
// partitioned by the given label names.
func NewGaugeIntVec(opts prometheus.GaugeOpts, labelNames []string) GaugeIntVec {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		labelNames,
		opts.ConstLabels,
	)
	return GaugeIntVec{
		v: prometheus.NewMetricVec(desc, func(lvs ...string) prometheus.Metric {
			return &GaugeInt{
				desc:       desc,
				labelPairs: prometheus.MakeLabelPairs(desc, lvs),
			}
		}),
	}
}

// NewCounterIntVec creates a new CounterIntVec based on the provided
// CounterOpts and partitioned by the given label names.
func NewCounterIntVec(opts prometheus.CounterOpts, labelNames []string) CounterIntVec {
	desc := prometheus.NewDesc(
		prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		labelNames,
		opts.ConstLabels,
	)
	return CounterIntVec{
		v: prometheus.NewMetricVec(desc, func(lvs ...string) prometheus.Metric {
			return &CounterInt{desc: desc, labelPairs: prometheus.MakeLabelPairs(desc, lvs)}
		}),
	}
}

func (v GaugeIntVec) Describe(ch chan<- *prometheus.Desc) { v.v.Describe(ch) }
func (v GaugeIntVec) Collect(ch chan<- prometheus.Metric) { v.v.Collect(ch) }
func (v GaugeIntVec) WithLabelValues(lvs ...string) *GaugeInt {
	return utils.OrPanic1(v.v.GetMetricWithLabelValues(lvs...)).(*GaugeInt)
}

func (v CounterIntVec) Describe(ch chan<- *prometheus.Desc) { v.v.Describe(ch) }
func (v CounterIntVec) Collect(ch chan<- prometheus.Metric) { v.v.Collect(ch) }
func (v CounterIntVec) WithLabelValues(lvs ...string) *CounterInt {
	return utils.OrPanic1(v.v.GetMetricWithLabelValues(lvs...)).(*CounterInt)
}
