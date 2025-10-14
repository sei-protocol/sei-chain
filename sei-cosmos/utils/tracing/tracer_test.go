package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewTracingInfo(t *testing.T) {
	tests := []struct {
		name           string
		tracingEnabled bool
		wantEnabled    int32
	}{
		{
			name:           "tracing enabled",
			tracingEnabled: true,
			wantEnabled:    1,
		},
		{
			name:           "tracing disabled",
			tracingEnabled: false,
			wantEnabled:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer := noop.NewTracerProvider().Tracer("test")
			info := NewTracingInfo(&tracer, tt.tracingEnabled)

			require.NotNil(t, info)
			require.NotNil(t, info.Tracer)
			require.Equal(t, tt.wantEnabled, info.tracingEnabled)
		})
	}
}

func TestInfo_Start_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(&tracer, false)

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
	info := NewTracingInfo(&tracer, true)

	ctx, span := info.Start("test-span")

	// When tracing is enabled, should return valid context and span
	require.NotNil(t, ctx)
	require.NotNil(t, span)
	require.NotEqual(t, NoOpSpan, span)

	span.End()
}

func TestInfo_StartWithContext_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(&tracer, false)

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
	info := NewTracingInfo(&tracer, true)

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
	info := NewTracingInfo(&tracer, true)

	// Should handle nil context gracefully
	ctx, span := info.StartWithContext("test-span", nil)

	require.NotNil(t, ctx)
	require.NotNil(t, span)

	span.End()
}

func TestInfo_GetContext_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(&tracer, false)

	// Set a context first
	testCtx := context.WithValue(context.Background(), "key", "value")
	info.SetContext(testCtx)

	// When tracing is disabled, GetContext should return background context
	ctx := info.GetContext()
	require.NotNil(t, ctx)
	require.Equal(t, context.Background(), ctx)
	require.Nil(t, ctx.Value("key")) // Should not have the value we set
}

func TestInfo_GetContext_EnabledTracing(t *testing.T) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(&tracer, true)

	// Set a context first
	testCtx := context.WithValue(context.Background(), "key", "value")
	info.SetContext(testCtx)

	// When tracing is enabled, GetContext should return the set context
	ctx := info.GetContext()
	require.NotNil(t, ctx)
	require.Equal(t, "value", ctx.Value("key"))
}

func TestInfo_SetContext_DisabledTracing(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(&tracer, false)

	// Set a context
	testCtx := context.WithValue(context.Background(), "key", "value")
	info.SetContext(testCtx)

	// When tracing is disabled, SetContext should be a no-op
	// The internal tracerContext should remain nil
	require.Nil(t, info.tracerContext)
}

func TestInfo_SetContext_EnabledTracing(t *testing.T) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(&tracer, true)

	// Set a context
	testCtx := context.WithValue(context.Background(), "key", "value")
	info.SetContext(testCtx)

	// When tracing is enabled, SetContext should update the internal context
	require.NotNil(t, info.tracerContext)
	require.Equal(t, "value", info.tracerContext.Value("key"))
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
	info := NewTracingInfo(&tracer, true)

	// Test concurrent access to ensure thread safety
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			ctx := context.WithValue(context.Background(), "id", idx)
			info.SetContext(ctx)
			_, span := info.Start("concurrent-span")
			span.End()
			_ = info.GetContext()
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
	info := NewTracingInfo(&tracer, false)

	// Perform many operations when tracing is disabled
	// The internal tracerContext should remain nil
	for i := 0; i < 100; i++ {
		testCtx := context.WithValue(context.Background(), "iteration", i)
		info.SetContext(testCtx)
		_, span := info.Start("test-span")
		span.End()
		_ = info.GetContext()
	}

	// Verify no context was stored
	require.Nil(t, info.tracerContext)
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

func BenchmarkInfo_Start_Disabled(b *testing.B) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(&tracer, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := info.Start("benchmark-span")
		span.End()
	}
}

func BenchmarkInfo_Start_Enabled(b *testing.B) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(&tracer, true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, span := info.Start("benchmark-span")
		span.End()
	}
}

func BenchmarkInfo_SetContext_Disabled(b *testing.B) {
	tracer := noop.NewTracerProvider().Tracer("test")
	info := NewTracingInfo(&tracer, false)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		info.SetContext(ctx)
	}
}

func BenchmarkInfo_SetContext_Enabled(b *testing.B) {
	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	info := NewTracingInfo(&tracer, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		info.SetContext(ctx)
	}
}
