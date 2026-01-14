package consensus

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Metrics of the producer state.
type Metrics struct {
	lastCommitQC         utils.Mutex[*utils.Option[time.Time]]
	tipCutFinality       prometheus.Histogram
	rpcClientLatency     *prometheus.HistogramVec
	commitQCLatencySum   *prometheus.GaugeVec
	commitQCLatencyCount *prometheus.GaugeVec
}

// NewMetrics creates a new Metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		lastCommitQC: utils.NewMutex(utils.Alloc(utils.None[time.Time]())),
		tipCutFinality: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "sei_stream_consensus__tipcut_finality",
			Help:    "latency of tipcut finality",
			Buckets: prometheus.ExponentialBuckets(0.01, 1.2, 35), // buckets start at 10 millisecond
		}),
		rpcClientLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "sei_stream_consensus__rpc_client_latency",
			Help:    "e2e latency of (successful) consensus RPCs",
			Buckets: prometheus.ExponentialBuckets(0.01, 1.2, 35),
		}, []string{"msg_type"}),
		commitQCLatencySum: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sei_stream_consensus__commit_qc_latency_sum",
			Help: "sum of tipcut finality timeouts",
		}, []string{"timeouts"}),
		commitQCLatencyCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sei_stream_consensus__commit_qc_latency_count",
			Help: "count of tipcut finality timeouts",
		}, []string{"timeouts"}),
	}
}

// ObserveCommitQC observes the CommitQC latency.
func (m *Metrics) ObserveCommitQC(qc *types.CommitQC) {
	now := time.Now()
	m.tipCutFinality.Observe(now.Sub(qc.Proposal().CreatedAt()).Seconds())
	for mLast := range m.lastCommitQC.Lock() {
		if last, ok := mLast.Get(); ok {
			label := strconv.FormatUint(uint64(qc.Proposal().View().Number), 10)
			m.commitQCLatencySum.WithLabelValues(label).Add(now.Sub(last).Seconds())
			m.commitQCLatencyCount.WithLabelValues(label).Add(1)
		}
		*mLast = utils.Some(now)
	}
}

// RPCClientLatency observer.
// Currently we use streaming RPC for consensus messages, so the standard gRPC metrics
// won't do.
func (m *Metrics) RPCClientLatency(msgType string) prometheus.Observer {
	return m.rpcClientLatency.WithLabelValues(msgType)
}

// Describe implements the prometheus.Collector interface.
func (s *State) Describe(c chan<- *prometheus.Desc) {}

// Collect implements the prometheus.Collector interface.
func (s *State) Collect(c chan<- prometheus.Metric) {
	s.metrics.tipCutFinality.Collect(c)
	s.metrics.commitQCLatencySum.Collect(c)
	s.metrics.commitQCLatencyCount.Collect(c)
	s.metrics.rpcClientLatency.Collect(c)
	s.avail.Collect(c)
}
