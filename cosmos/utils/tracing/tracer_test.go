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
