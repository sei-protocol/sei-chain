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

var meter = otel.Meter("tendermint_internal_p2p_rpc")
var latency = utils.OrPanic1(meter.Float64Histogram("latency", metric.WithUnit("s"), buckets(0.001, 1.3, 30)))
var inFlight = utils.OrPanic1(meter.Int64UpDownCounter("inflight"))
var sendMsgs = utils.OrPanic1(meter.Int64UpDownCounter("send_msgs"))
var sendBytes = utils.OrPanic1(meter.Int64UpDownCounter("send_bytes", metric.WithUnit("B")))
var recvMsgs = utils.OrPanic1(meter.Int64UpDownCounter("recv_msgs"))
var recvBytes = utils.OrPanic1(meter.Int64UpDownCounter("recv_bytes", metric.WithUnit("B")))

type Role string

const RoleServer = Role("server")
const RoleClient = Role("client")

type Attrs metric.MeasurementOption

func NewAttrs(role Role, rpcName string) Attrs {
	return Attrs(metric.WithAttributeSet(attribute.NewSet(
		attribute.String("role", string(role)),
		attribute.String("rpc_name", rpcName),
	)))
}

type Call struct {
	opts  metric.MeasurementOption
	start time.Time
}

func StartCall(attrs Attrs) Call {
	ctx := context.Background()
	opts := metric.MeasurementOption(attrs)
	inFlight.Add(ctx, 1, opts)
	return Call{opts, time.Now()}
}

func (c Call) Send(size int) {
	ctx := context.Background()
	sendMsgs.Add(ctx, 1, c.opts)
	sendBytes.Add(ctx, int64(size), c.opts)
}

func (c Call) Recv(size int) {
	ctx := context.Background()
	recvMsgs.Add(ctx, 1, c.opts)
	recvBytes.Add(ctx, int64(size), c.opts)
}

func (c Call) Stop() {
	ctx := context.Background()
	inFlight.Add(ctx, -1, c.opts)
	latency.Record(ctx, time.Since(c.start).Seconds(), c.opts)
}
