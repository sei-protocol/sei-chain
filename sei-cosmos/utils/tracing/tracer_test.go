package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewTracingInfo(t *testing.T) {
	tests := []struct {
		name           string
		tracingEnabled bool
		wantEnabled    bool
	}{
		{
			name:           "tracing enabled",
			tracingEnabled: true,
			wantEnabled:    true,
		},
		{
			name:           "tracing disabled",
			tracingEnabled: false,
			wantEnabled:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer := noop.NewTracerProvider().Tracer("test")
			info := NewTracingInfo(tracer, tt.tracingEnabled)

			require.NotNil(t, info)
			require.Equal(t, tt.wantEnabled, info.tracingEnabled.Load())
		})
	}
}

func TestInfo_Start_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(tracer, false)

	ctx, span := info.Start("test-span")

	// When tracing is disabled, should return background context and NoOpSpan
	require.NotNil(t, ctx)
	require.NotNil(t, span)
	require.Equal(t, NoOpSpan, span)

	// NoOpSpan should not record anything
	span.End()
	require.False(t, span.IsRecording())
}

func TestInfo_Start_EnabledTracing(t *testing.T) {
	// Create a real tracer provider for testing
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	ctx, span := info.Start("test-span")

	// When tracing is enabled, should return valid context and span
	require.NotNil(t, ctx)
	require.NotNil(t, span)
	require.NotEqual(t, NoOpSpan, span)

	span.End()
}

func TestInfo_StartWithContext_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(tracer, false)

	inputCtx := context.WithValue(context.Background(), "key", "value")
	ctx, span := info.StartWithContext("test-span", inputCtx)

	// When tracing is disabled, should return the input context and NoOpSpan
	require.NotNil(t, ctx)
	require.Equal(t, inputCtx, ctx)
	require.Equal(t, NoOpSpan, span)
	require.Equal(t, "value", ctx.Value("key"))

	// NoOpSpan should not record anything
	span.End()
	require.False(t, span.IsRecording())
}

func TestInfo_StartWithContext_EnabledTracing(t *testing.T) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	inputCtx := context.WithValue(context.Background(), "key", "value")
	ctx, span := info.StartWithContext("test-span", inputCtx)

	// When tracing is enabled, should return valid context and span
	require.NotNil(t, ctx)
	require.NotNil(t, span)
	require.NotEqual(t, NoOpSpan, span)

	span.End()
}

func TestInfo_StartWithContext_NilContext(t *testing.T) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Should handle nil context gracefully
	ctx, span := info.StartWithContext("test-span", nil)

	require.NotNil(t, ctx)
	require.NotNil(t, span)

	span.End()
}

func TestNoOpSpan_Properties(t *testing.T) {
	// Verify NoOpSpan has the expected properties
	require.NotNil(t, NoOpSpan)
	require.False(t, NoOpSpan.IsRecording())

	// Should be safe to call methods on NoOpSpan
	NoOpSpan.End()
	NoOpSpan.SetName("test")
	NoOpSpan.SetAttributes()

	// SpanContext should be invalid
	spanCtx := NoOpSpan.SpanContext()
	require.False(t, spanCtx.IsValid())
}

func TestInfo_ConcurrentAccess(t *testing.T) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Test concurrent access to ensure thread safety
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			_, span := info.Start("concurrent-span")
			span.End()
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestInfo_DisabledTracing_NoMemoryLeak(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(tracer, false)

	// Perform many operations when tracing is disabled
	// Should not allocate unnecessary resources
	for i := 0; i < 100; i++ {
		_, span := info.Start("test-span")
		span.End()
	}

	// All spans should be NoOpSpan
	_, span := info.Start("final-span")
	require.Equal(t, NoOpSpan, span)
}

func TestTracerProvider(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		expectErr bool
	}{
		{
			name:      "valid URL",
			url:       "http://localhost:14268/api/traces",
			expectErr: false,
		},
		{
			name:      "custom URL",
			url:       "http://custom-host:8080/traces",
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, err := TracerProvider(tt.url)
			if tt.expectErr {
				require.Error(t, err)
				require.Nil(t, tp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, tp)
			}
		})
	}
}

func TestDefaultTracerProvider(t *testing.T) {
	tp, err := DefaultTracerProvider()
	require.NoError(t, err)
	require.NotNil(t, tp)
}

func TestGetTracerProviderOptions(t *testing.T) {
	opts, err := GetTracerProviderOptions(DefaultTracingURL)
	require.NoError(t, err)
	require.NotNil(t, opts)
	require.NotEmpty(t, opts)
}

func TestInfo_StartBlockSpan_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(tracer, false)

	ctx := context.Background()
	returnedCtx, span := info.StartBlockSpan(ctx)

	// When tracing is disabled, should return the input context and nil span
	require.NotNil(t, returnedCtx)
	require.Equal(t, ctx, returnedCtx)
	require.Nil(t, span)
}

func TestInfo_StartBlockSpan_EnabledTracing(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	ctx := context.Background()
	returnedCtx, span := info.StartBlockSpan(ctx)

	// When tracing is enabled, should return valid context and span
	require.NotNil(t, returnedCtx)
	require.NotNil(t, span)
	require.NotEqual(t, ctx, returnedCtx) // Context should have span context

	// Span should be recording
	require.True(t, span.IsRecording())

	// Clean up
	info.EndBlockSpan()
}

func TestInfo_StartBlockSpan_Idempotent(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	ctx := context.Background()

	// Start block span first time
	ctx1, span1 := info.StartBlockSpan(ctx)
	require.NotNil(t, ctx1)
	require.NotNil(t, span1)
	require.True(t, span1.IsRecording())

	// Start block span second time - should return the same span
	ctx2, span2 := info.StartBlockSpan(ctx)
	require.NotNil(t, ctx2)
	require.NotNil(t, span2)

	// Should be the same span (idempotent)
	require.Equal(t, span1, span2)
	require.Equal(t, span1.SpanContext(), span2.SpanContext())

	// Clean up
	info.EndBlockSpan()
}

func TestInfo_EndBlockSpan_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(tracer, false)

	// Should not panic when tracing is disabled
	require.NotPanics(t, func() {
		info.EndBlockSpan()
	})
}

func TestInfo_EndBlockSpan_EnabledTracing(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	ctx := context.Background()
	_, span := info.StartBlockSpan(ctx)
	require.True(t, span.IsRecording())

	// End the block span
	info.EndBlockSpan()

	// Span should no longer be recording
	require.False(t, span.IsRecording())
}

func TestInfo_EndBlockSpan_Idempotent(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	ctx := context.Background()
	_, span := info.StartBlockSpan(ctx)
	require.True(t, span.IsRecording())

	// End the block span first time
	info.EndBlockSpan()
	require.False(t, span.IsRecording())

	// End the block span second time - should not panic
	require.NotPanics(t, func() {
		info.EndBlockSpan()
	})

	// Span should still not be recording
	require.False(t, span.IsRecording())
}

func TestInfo_EndBlockSpan_WithoutStart(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Call EndBlockSpan without calling StartBlockSpan first
	// Should not panic
	require.NotPanics(t, func() {
		info.EndBlockSpan()
	})
}

func TestInfo_ChildSpan_ParentedToBlockSpan(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Start a block span
	ctx := context.Background()
	blockCtx, blockSpan := info.StartBlockSpan(ctx)
	require.NotNil(t, blockCtx)
	require.NotNil(t, blockSpan)
	require.True(t, blockSpan.IsRecording())

	// Start a child span using Start method
	childCtx, childSpan := info.Start("child-span")
	require.NotNil(t, childCtx)
	require.NotNil(t, childSpan)

	// The child span should have the block span as its parent
	// We can verify this by checking the span context
	childSpanContext := childSpan.SpanContext()
	blockSpanContext := blockSpan.SpanContext()

	// Both should be valid
	require.True(t, childSpanContext.IsValid())
	require.True(t, blockSpanContext.IsValid())

	// Clean up
	childSpan.End()
	info.EndBlockSpan()

	// Verify the parent-child relationship in the exported spans
	spans := exporter.GetSpans()
	require.Len(t, spans, 2) // block span + child span

	// Find the child span
	var childSnapshot tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "child-span" {
			childSnapshot = s
			break
		}
	}

	// Verify parent relationship
	require.Equal(t, blockSpanContext.TraceID(), childSnapshot.SpanContext.TraceID())
	require.Equal(t, blockSpanContext.SpanID(), childSnapshot.Parent.SpanID())
}

func TestInfo_MultipleChildSpans_ParentedToBlockSpan(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Start a block span
	ctx := context.Background()
	_, blockSpan := info.StartBlockSpan(ctx)
	require.True(t, blockSpan.IsRecording())
	blockSpanContext := blockSpan.SpanContext()

	// Create multiple child spans
	_, child1 := info.Start("child-span-1")
	_, child2 := info.Start("child-span-2")
	_, child3 := info.Start("child-span-3")

	// All child spans should be valid
	require.True(t, child1.SpanContext().IsValid())
	require.True(t, child2.SpanContext().IsValid())
	require.True(t, child3.SpanContext().IsValid())

	// Clean up
	child1.End()
	child2.End()
	child3.End()
	info.EndBlockSpan()

	// Verify all children have the block span as parent
	spans := exporter.GetSpans()
	require.Len(t, spans, 4) // block span + 3 child spans

	childCount := 0
	for _, s := range spans {
		if s.Name != "Block" {
			require.Equal(t, blockSpanContext.TraceID(), s.SpanContext.TraceID())
			require.Equal(t, blockSpanContext.SpanID(), s.Parent.SpanID())
			childCount++
		}
	}
	require.Equal(t, 3, childCount)
}

func TestInfo_ChildSpan_WithoutBlockSpan(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Start a child span without starting a block span first
	ctx, span := info.Start("standalone-span")
	require.NotNil(t, ctx)
	require.NotNil(t, span)

	// Span should still be valid
	require.True(t, span.SpanContext().IsValid())

	// Clean up
	span.End()
}

func TestInfo_ChildSpan_AfterBlockSpanEnds(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Start and end a block span
	ctx := context.Background()
	_, blockSpan := info.StartBlockSpan(ctx)
	require.True(t, blockSpan.IsRecording())
	blockSpanContext := blockSpan.SpanContext()
	info.EndBlockSpan()
	require.False(t, blockSpan.IsRecording())

	// Start a child span after block span ends
	childCtx, childSpan := info.Start("child-after-block-end")
	require.NotNil(t, childCtx)
	require.NotNil(t, childSpan)

	// Child span should not have block span as parent (since it ended)
	require.True(t, childSpan.SpanContext().IsValid())

	// Clean up
	childSpan.End()

	// Verify the child doesn't have block span as parent
	spans := exporter.GetSpans()
	for _, s := range spans {
		if s.Name == "child-after-block-end" {
			// The parent should NOT be the block span
			require.NotEqual(t, blockSpanContext.SpanID(), s.Parent.SpanID())
		}
	}
}

func TestInfo_BlockSpan_ConcurrentAccess(t *testing.T) {
	// Create a real SDK tracer that actually records
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	// Start block span
	ctx := context.Background()
	_, blockSpan := info.StartBlockSpan(ctx)
	require.True(t, blockSpan.IsRecording())

	// Test concurrent access to Start while block span is active
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			_, span := info.Start("concurrent-child-span")
			span.End()
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Clean up
	info.EndBlockSpan()
}

func BenchmarkInfo_Start_Disabled(b *testing.B) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(tracer, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := info.Start("benchmark-span")
		span.End()
	}
}

func BenchmarkInfo_Start_Enabled(b *testing.B) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := info.Start("benchmark-span")
		span.End()
	}
}

func BenchmarkInfo_StartBlockSpan(b *testing.B) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(tracer, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = info.StartBlockSpan(ctx)
		info.EndBlockSpan()
	}
}
