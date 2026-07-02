package metrics

import (
	"context"
	"time"
	"sync/atomic"

	pb "github.com/prometheus/client_model/go"
	prometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type Sink struct {
	ch chan<- prometheus.Metric
	labelNames []string
	labelValues []string
}

func (s Sink) With(name, value string) Sink {
	s.labelNames = append(s.labelNames, name)
	s.labelValues = append(s.labelValues, value)
	return s
}

func (s Sink) Counter(name, help string, value float64) {
	s.push(prometheus.CounterValue, name, help, value)
}

func (s Sink) Gauge(name, help string, value float64) {
	s.push(prometheus.GaugeValue, name, help, value)
}

type labeledHistogram struct {
	h prometheus.Histogram
}

func (h labeledHistogram) Desc() *prometheus.Desc {
	
}

func (s Sink) Histogram(name, help string, h prometheus.Histogram) {
	// TODO
	s.ch <- h	
}

func (s Sink) push(t prometheus.ValueType, name, help string, value float64) {
	desc := prometheus.NewDesc(name, help, s.labelNames, nil)
	s.ch <- prometheus.MustNewConstMetric(desc, t, value, s.labelValues...)
}

type Collector interface { Push(sink Sink) }

type Labels interface { comparable; Add(Sink) Sink }

type Map[L Labels, M Collector] struct { m *utils.RWMutex[map[L]M] }

func NewMap[L Labels, C Collector]() Map[L,C] {
	return Map[L,C]{m: utils.Alloc(utils.NewRWMutex(map[L]C{}))}
}

func (m Map[L,C]) Describe(chan<-*prometheus.Desc) {}
func (m Map[L,C]) Collect(ch chan<- prometheus.Metric) { m.Push(Sink{ch: ch}) }
func (m Map[L,C]) Push(sink Sink) {
	for m := range m.m.RLock() {
		for l,x := range m {
			x.Push(l.Add(sink))
		}
	}
}

func buckets(start float64, factor float64, count int) []float64 {
	return prometheus.ExponentialBuckets(start, factor, count)
}

type Role string
const RoleAccept = Role("accept")
const RoleConnect = Role("connect")

type Attrs struct {
	Role Role
	RPCName string
}

func (a Attrs) Add(s Sink) Sink {
	return s.With("role",string(a.Role)).With("rpc_name",a.RPCName)
}

type stream struct {
	latency prometheus.Histogram
	inFlight atomic.Uint64 
	sendMsgs atomic.Uint64
	sendBytes atomic.Uint64 
	recvMsgs atomic.Uint64
	recvBytes atomic.Uint64
}

func (s *stream) Push(sink Sink) {
	sink.Histogram("tendermint_internal_p2p_mux__latency","",s.latency)
	sink.Gauge("tendermint_internal_p2p_mux__inflight","",float64(s.inFlight.Load()))
	sink.Counter("tendermint_internal_p2p_mux__send_msgs","",float64(s.sendMsgs.Load()))
	sink.Counter("tendermint_internal_p2p_mux__send_bytes","",float64(s.sendBytes.Load()))
	sink.Counter("tendermint_internal_p2p_mux__recv_msgs","",float64(s.recvMsgs.Load()))
	sink.Counter("tendermint_internal_p2p_mux__recv_bytes","",float64(s.recvBytes.Load()))
}

type chainID string
func (c chainID) Add(s Sink) Sink { return s.With("chain_id",string(c)) }

var all = Map[chainID,Map[Attrs,*stream]]{}

func init() {
	prometheus.MustRegister(all)	
}

func newStreamMetrics() *stream {
	return &stream {
		latency: prometheus.NewHistogram(prometheus.HistogramOpts{Buckets: buckets(0.001, 1.3, 30)}),
	}
}

var latency = utils.OrPanic1(meter.Float64Histogram("tendermint_internal_p2p_mux__latency", metric.WithUnit("s"), buckets(0.001, 1.3, 30)))
var inFlight = utils.OrPanic1(meter.Int64UpDownCounter())
var sendMsgs = utils.OrPanic1(meter.Int64Counter())
var sendBytes = utils.OrPanic1(meter.Int64Counter(, metric.WithUnit("B")))
var recvMsgs = utils.OrPanic1(meter.Int64Counter("tendermint_internal_p2p_mux__recv_msgs"))
var recvBytes = utils.OrPanic1(meter.Int64Counter("tendermint_internal_p2p_mux__recv_bytes", metric.WithUnit("B")))

type Stream struct {
	opts  metric.MeasurementOption
	start utils.Option[time.Time]
}

func NewStream(attrs Attrs) *Stream {
	return &Stream{opts: metric.MeasurementOption(attrs)}
}

func (s *Stream) Open() {
	if s.start.IsPresent() {
		return
	}
	s.start = utils.Some(time.Now())
	ctx := context.Background()
	inFlight.Add(ctx, 1, s.opts)
}

func (s *Stream) Send(size int) {
	ctx := context.Background()
	sendMsgs.Add(ctx, 1, s.opts)
	sendBytes.Add(ctx, int64(size), s.opts)
}

func (s *Stream) Recv(size int) {
	ctx := context.Background()
	recvMsgs.Add(ctx, 1, s.opts)
	recvBytes.Add(ctx, int64(size), s.opts)
}

func (s *Stream) Close() {
	if start, ok := s.start.Get(); ok {
		ctx := context.Background()
		inFlight.Add(ctx, -1, s.opts)
		latency.Record(ctx, time.Since(start).Seconds(), s.opts)
		s.start = utils.None[time.Time]()
	}
}
