package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	otrace "go.opentelemetry.io/otel/trace"
)

const DefaultTracingURL = "http://localhost:14268/api/traces"

func DefaultTracerProvider() (*trace.TracerProvider, error) {
	return TracerProvider(DefaultTracingURL)
}

func TracerProvider(url string) (*trace.TracerProvider, error) {
	// Create the Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, err
	}
	tp := trace.NewTracerProvider(
		// Always be sure to batch in production.
		trace.WithBatcher(exp),
		// Record information about this application in a Resource.
		trace.WithResource(resource.NewWithAttributes(
			"https://opentelemetry.io/schemas/1.9.0",
			attribute.Key("service.name").String("sei-chain"),
			attribute.String("environment", "production"),
			attribute.Int64("ID", 1),
		)),
	)
	return tp, nil
}

type Info struct {
	Tracer        *otrace.Tracer
	TracerContext context.Context
	BlockSpan     *otrace.Span
}
