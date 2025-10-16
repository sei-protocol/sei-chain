package tracing

import (
	"context"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	otrace "go.opentelemetry.io/otel/trace"
)

const DefaultTracingURL = "http://localhost:14268/api/traces"
const FlagTracing = "tracing"

func DefaultTracerProvider() (*trace.TracerProvider, error) {
	return TracerProvider(DefaultTracingURL)
}

func TracerProvider(url string) (*trace.TracerProvider, error) {
	// Create the Jaeger exporter
	opts, err := GetTracerProviderOptions(url)
	if err != nil {
		return nil, err
	}
	tp := trace.NewTracerProvider(opts...)
	return tp, nil
}

func GetTracerProviderOptions(url string) ([]trace.TracerProviderOption, error) {
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, err
	}
	return []trace.TracerProviderOption{
		// Always be sure to batch in production.
		trace.WithBatcher(exp),
		// Record information about this application in a Resource.
		trace.WithResource(resource.NewWithAttributes(
			"https://opentelemetry.io/schemas/1.9.0",
			attribute.Key("service.name").String("sei-chain"),
			attribute.String("environment", "production"),
			attribute.Int64("ID", 1),
		)),
	}, nil
}

type Info struct {
	tracer         otrace.Tracer
	blockSpan      otrace.Span
	tracingEnabled atomic.Bool
	mtx            sync.RWMutex
}

func NewTracingInfo(tr otrace.Tracer, tracingEnabled bool) *Info {
	info := &Info{
		tracer:         tr,
		blockSpan:      NoOpSpan,
		tracingEnabled: atomic.Bool{},
	}
	info.tracingEnabled.Store(tracingEnabled)
	return info
}

// NoOpSpan is a no-op span which does nothing.
var NoOpSpan = otrace.SpanFromContext(context.TODO())

func (i *Info) Start(name string) (context.Context, otrace.Span) {
	if !i.tracingEnabled.Load() {
		return context.Background(), NoOpSpan
	}
	i.mtx.Lock()
	defer i.mtx.Unlock()
	// if we have a started block span, we can use that as a parent span
	ctx := context.Background()
	if i.blockSpan.IsRecording() {
		ctx = otrace.ContextWithSpanContext(ctx, i.blockSpan.SpanContext())
	}
	return i.tracer.Start(ctx, name)
}

func (i *Info) StartWithContext(name string, ctx context.Context) (context.Context, otrace.Span) {
	if !i.tracingEnabled.Load() {
		return ctx, NoOpSpan
	}
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	return i.tracer.Start(ctx, name)
}

func (i *Info) StartBlockSpan(c context.Context) (context.Context, otrace.Span) {
	if !i.tracingEnabled.Load() {
		return c, NoOpSpan
	}
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if i.blockSpan.IsRecording() { // already started
		return c, i.blockSpan
	}
	ctx, span := i.tracer.Start(c, "Block")
	i.blockSpan = span
	return ctx, i.blockSpan

}

func (i *Info) EndBlockSpan() {
	if !i.tracingEnabled.Load() {
		return
	}
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if !i.blockSpan.IsRecording() { // already ended
		return
	}
	i.blockSpan.End()
	i.blockSpan = NoOpSpan
}
