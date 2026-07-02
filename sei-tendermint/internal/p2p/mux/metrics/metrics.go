package metrics

import (
	"time"

	prometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func buckets(start float64, factor float64, count int) []float64 {
	return prometheus.ExponentialBuckets(start, factor, count)
}

type Role string
const RoleAccept = Role("accept")
const RoleConnect = Role("connect")

type Labels []string

func (l Labels) Histogram(opts prometheus.HistogramOpts) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(opts,l)
	prometheus.MustRegister(h)
	return h
}

func (l Labels) Counter(opts prometheus.CounterOpts) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(opts,l)
	prometheus.MustRegister(c)
	return c
}

func (l Labels) Gauge(opts prometheus.GaugeOpts) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(opts,l)
	prometheus.MustRegister(g)
	return g

}

type Attrs struct {
	ChainID string
	Role Role
	RPCName string
}

var labels = Labels{"chain_id","role","rpc_name"}

const subsystem = "tendermint_internal_p2p_mux_"

var latency = labels.Histogram(prometheus.HistogramOpts{Subsystem: subsystem, Name: "latency", Buckets: buckets(0.001, 1.3, 30)})
var inFlight = labels.Gauge(prometheus.GaugeOpts{Subsystem: subsystem, Name: "inflight"})
var sendMsgs = labels.Counter(prometheus.CounterOpts{Subsystem: subsystem, Name: "send_msgs"})
var recvMsgs = labels.Counter(prometheus.CounterOpts{Subsystem: subsystem, Name: "recv_msgs"})
var sendBytes = labels.Counter(prometheus.CounterOpts{Subsystem: subsystem, Name: "send_bytes"})
var recvBytes = labels.Counter(prometheus.CounterOpts{Subsystem: subsystem, Name: "recv_bytes"})

func (a Attrs) Metrics() *Metrics {
	v := []string{a.ChainID,string(a.Role),a.RPCName}
	return &Metrics {
		latency: latency.WithLabelValues(v...),
		inFlight: inFlight.WithLabelValues(v...),
		sendMsgs: sendMsgs.WithLabelValues(v...),
		recvMsgs: recvMsgs.WithLabelValues(v...),
		sendBytes: sendBytes.WithLabelValues(v...),
		recvBytes: recvBytes.WithLabelValues(v...),
	}
}

type Metrics struct {
	latency prometheus.Observer
	inFlight prometheus.Gauge
	sendMsgs prometheus.Counter
	recvMsgs prometheus.Counter
	sendBytes prometheus.Counter
	recvBytes prometheus.Counter
}

type Stream struct {
	m *Metrics	
	start utils.Option[time.Time]
}

func NewStream(m *Metrics) *Stream { return &Stream{m:m} }

func (s *Stream) Open() {
	if s.start.IsPresent() {
		return
	}
	s.start = utils.Some(time.Now())
	s.m.inFlight.Inc()
}

func (s *Stream) Send(size int) {
	s.m.sendMsgs.Inc()
	s.m.sendBytes.Add(float64(size))
}

func (s *Stream) Recv(size int) {
	s.m.recvMsgs.Inc()
	s.m.recvBytes.Inc()
}

func (s *Stream) Close() {
	if start, ok := s.start.Get(); ok {
		s.m.inFlight.Dec()
		s.m.latency.Observe(time.Since(start).Seconds())
	}
}
