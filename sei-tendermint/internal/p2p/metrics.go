package p2p

import (
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const (
	// MetricsNamespace is the namespace shared by all Tendermint Prometheus metrics.
	MetricsNamespace = "tendermint"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "p2p"
)

var (
	// valueToLabelRegexp is used to find the golang package name and type name
	// so that the name can be turned into a prometheus label where the characters
	// in the label do not include prometheus special characters such as '*' and '.'.
	valueToLabelRegexp = regexp.MustCompile(`\*?(\w+)\.(.*)`)
)

//go:generate go run ../../scripts/metricsgen -struct=Metrics

// Metrics contains metrics exposed by this package.
type Metrics struct {
	// Number of peers.
	peers prometheus.GaugeIntVec
	// Number of bytes per channel received from a given peer.
	peerReceiveBytesTotal prometheus.CounterIntVec `metrics_labels:"peer_id, chID, message_type"`
	// Number of newly established connections.
	newConnections prometheus.CounterIntVec `metrics_labels:"direction, success"`

	// RouterPeerQueueRecv defines the time taken to read off of a peer's queue
	// before sending on the connection.
	//metrics:The time taken to read off of a peer's queue before sending on the connection.
	routerPeerQueueRecv prometheus.HistogramVec

	channelMsgs prometheus.CounterIntVec `metrics_labels:"ch_id, direction"`

	// QueueDroppedMsgs counts the messages dropped from the router's queues.
	//metrics:The number of messages dropped from router's queues.
	queueDroppedMsgs prometheus.CounterIntVec `metrics_labels:"ch_id, direction"`

	// Number of live giga p2p connections.
	gigaConns prometheus.GaugeIntVec `metrics_labels:"direction"`
	// Counts established giga p2p connections.
	gigaNewConns prometheus.CounterIntVec `metrics_labels:"direction"`
}

type metricsLabelCache struct {
	mtx               sync.RWMutex
	messageLabelNames map[reflect.Type]string
}

// ValueToMetricLabel is a method that is used to produce a prometheus label value of the golang
// type that is passed in.
// This method uses a map on the Metrics struct so that each label name only needs
// to be produced once to prevent expensive string operations.
func (m *metricsLabelCache) ValueToMetricLabel(i any) string {
	t := reflect.TypeOf(i)
	m.mtx.RLock()

	if s, ok := m.messageLabelNames[t]; ok {
		m.mtx.RUnlock()
		return s
	}
	m.mtx.RUnlock()

	s := t.String()
	ss := valueToLabelRegexp.FindStringSubmatch(s)
	l := fmt.Sprintf("%s_%s", ss[1], ss[2])
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.messageLabelNames[t] = l
	return l
}

func newMetricsLabelCache() *metricsLabelCache {
	return &metricsLabelCache{
		messageLabelNames: map[reflect.Type]string{},
	}
}
