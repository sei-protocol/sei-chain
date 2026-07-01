package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	prometheus "github.com/prometheus/client_golang/prometheus"
	
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func buckets(start float64, factor float64, count int) metric.HistogramOption {
	return metric.WithExplicitBucketBoundaries(prometheus.ExponentialBuckets(start, factor, count)...)
}

var meter = otel.Meter("tendermint_internal_p2p_mux")
var latency = utils.OrPanic1(meter.Float64Histogram("latency", metric.WithUnit("s"), buckets(0.001, 1.3, 30)))
var inFlight = utils.OrPanic1(meter.Int64UpDownCounter("inflight"))
var sendMsgs = utils.OrPanic1(meter.Int64UpDownCounter("send_msgs"))
var sendBytes = utils.OrPanic1(meter.Int64UpDownCounter("send_bytes", metric.WithUnit("B")))
var recvMsgs = utils.OrPanic1(meter.Int64UpDownCounter("recv_msgs"))
var recvBytes = utils.OrPanic1(meter.Int64UpDownCounter("recv_bytes", metric.WithUnit("B")))

type Role string

const RoleAccept = Role("accept")
const RoleConnect = Role("connect")

type Attrs metric.MeasurementOption

func NewAttrs(role Role, rpcName string) Attrs {
	return Attrs(metric.WithAttributeSet(attribute.NewSet(
		attribute.String("role", string(role)),
		attribute.String("rpc_name", rpcName),
	)))
}

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
