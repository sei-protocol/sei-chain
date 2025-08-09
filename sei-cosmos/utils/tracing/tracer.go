package tracing

import (
	"context"
	"sync"

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
	Tracer        *otrace.Tracer
	tracerContext context.Context
	BlockSpan     *otrace.Span

	mtx sync.RWMutex
}

func (i *Info) Start(name string) (context.Context, otrace.Span) {
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if i.tracerContext == nil {
		i.tracerContext = context.Background()
	}
	return (*i.Tracer).Start(i.tracerContext, name)
}

func (i *Info) StartWithContext(name string, ctx context.Context) (context.Context, otrace.Span) {
	i.mtx.Lock()
	defer i.mtx.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	return (*i.Tracer).Start(ctx, name)
}

func (i *Info) GetContext() context.Context {
	i.mtx.RLock()
	defer i.mtx.RUnlock()
	return i.tracerContext
}

func (i *Info) SetContext(c context.Context) {
	i.mtx.Lock()
	defer i.mtx.Unlock()
	i.tracerContext = c
}
