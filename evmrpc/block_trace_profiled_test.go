package evmrpc

import (
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/stretchr/testify/require"
)

func TestShouldUseProfiledBlockTrace(t *testing.T) {
	t.Parallel()

	defaultTracer := ""
	callTracer := "callTracer"

	tests := []struct {
		name     string
		enabled  bool
		config   *tracers.TraceConfig
		expected bool
	}{
		{
			name:     "disabled by default with nil config",
			enabled:  false,
			config:   nil,
			expected: false,
		},
		{
			name:    "disabled by default with default tracer",
			enabled: false,
			config: &tracers.TraceConfig{
				Tracer: &defaultTracer,
			},
			expected: false,
		},
		{
			name:     "enabled with nil config",
			enabled:  true,
			config:   nil,
			expected: true,
		},
		{
			name:    "enabled with default tracer",
			enabled: true,
			config: &tracers.TraceConfig{
				Tracer: &defaultTracer,
			},
			expected: true,
		},
		{
			name:    "explicit tracer keeps legacy path",
			enabled: true,
			config: &tracers.TraceConfig{
				Tracer: &callTracer,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := &DebugAPI{profiledBlockTrace: tt.enabled}
			require.Equal(t, tt.expected, api.shouldUseProfiledBlockTrace(tt.config))
		})
	}
}
