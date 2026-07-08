package metrics

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmprometheus "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_p2p_mux"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Latency from Open() to Close() of the stream. Relevant only for short lived streams.
	latency tmprometheus.HistogramVec `metrics_labels:"role, kind" metrics_buckets:"exp(0.001, 1.3, 30)"`
	// Number of currently open streams.
	inFlight tmprometheus.GaugeIntVec `metrics_labels:"role, kind"`
	// Number of messages sent.
	sendMsgs tmprometheus.CounterIntVec `metrics_labels:"role, kind"`
	// Number of messages received.
	recvMsgs tmprometheus.CounterIntVec `metrics_labels:"role, kind"`
	// Numbed of message bytes sent.
	sendBytes tmprometheus.CounterIntVec `metrics_labels:"role, kind"`
	// Number of message bytes received.
	recvBytes tmprometheus.CounterIntVec `metrics_labels:"role, kind"`
}

type Role string

const RoleAccept = Role("accept")
const RoleConnect = Role("connect")

type Metrics struct {
	latency   *tmprometheus.Histogram
	inFlight  *tmprometheus.GaugeInt
	sendMsgs  *tmprometheus.CounterInt
	recvMsgs  *tmprometheus.CounterInt
	sendBytes *tmprometheus.CounterInt
	recvBytes *tmprometheus.CounterInt
}

func Get(role Role, kind string) *Metrics {
	return &Metrics{
		latency:   Global.latencyAt(string(role), kind),
		inFlight:  Global.inFlightAt(string(role), kind),
		sendMsgs:  Global.sendMsgsAt(string(role), kind),
		recvMsgs:  Global.recvMsgsAt(string(role), kind),
		sendBytes: Global.sendBytesAt(string(role), kind),
		recvBytes: Global.recvBytesAt(string(role), kind),
	}
}

type Stream struct {
	m     *Metrics
	start utils.Option[time.Time]
}

func NewStream(m *Metrics) *Stream { return &Stream{m: m} }

func (s *Stream) Open() {
	if s.start.IsPresent() {
		return
	}
	s.start = utils.Some(time.Now())
	s.m.inFlight.Add(1)
}

func (s *Stream) Send(size int) {
	s.m.sendMsgs.Add(1)
	s.m.sendBytes.Add(int64(size))
}

func (s *Stream) Recv(size int) {
	s.m.recvMsgs.Add(1)
	s.m.recvBytes.Add(int64(size))
}

func (s *Stream) Close() {
	if start, ok := s.start.Get(); ok {
		s.m.inFlight.Add(-1)
		s.m.latency.Observe(time.Since(start).Seconds())
	}
}
