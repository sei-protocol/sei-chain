package evmrpc

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/stretchr/testify/require"
)

func TestValidateTraceTracerAllowlist(t *testing.T) {
	api := &DebugAPI{
		allowedTracers: buildAllowedTracerSet([]string{
			callTracerName,
			prestateTracerName,
			muxTracerName,
		}),
	}

	require.NoError(t, api.validateTraceTracer(nil))
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{}))

	name := callTracerName
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}))

	name = flatCallTracerName
	require.ErrorContains(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}), "is not allowed")

	name = "function() { return {}; }"
	require.ErrorContains(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}), "JavaScript tracers are disabled")

	name = ""
	require.ErrorContains(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}), "must not be empty")
}

func TestValidateTraceTracerAllowsJSWhenConfigured(t *testing.T) {
	api := &DebugAPI{
		allowedTracers: buildAllowedTracerSet([]string{
			callTracerName,
			muxTracerName,
		}),
		allowJSTracers: true,
	}

	name := "function() { return {}; }"
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}))

	name = flatCallTracerName
	require.ErrorContains(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}), "native tracer")

	name = ""
	require.ErrorContains(t, api.validateTraceTracer(&tracers.TraceConfig{Tracer: &name}), "must not be empty")
}

func TestValidateMuxTraceConfig(t *testing.T) {
	api := &DebugAPI{
		allowedTracers: buildAllowedTracerSet([]string{
			callTracerName,
			prestateTracerName,
			muxTracerName,
		}),
	}

	name := muxTracerName
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{"callTracer":{},"prestateTracer":{}}`),
	}))
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{"muxTracer":{"callTracer":{}}}`),
	}))

	err := api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{"function() { return {}; }":{}}`),
	})
	require.ErrorContains(t, err, "nested debug tracer")
	require.ErrorContains(t, err, "JavaScript tracers are disabled")

	err = api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{"muxTracer":{"badTracer":{}}}`),
	})
	require.ErrorContains(t, err, "nested debug tracer")
	require.ErrorContains(t, err, "badTracer")
}

func TestValidateMuxTraceConfigAllowsJSWhenConfigured(t *testing.T) {
	api := &DebugAPI{
		allowedTracers: buildAllowedTracerSet([]string{
			callTracerName,
			muxTracerName,
		}),
		allowJSTracers: true,
	}

	name := muxTracerName
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{"function() { return {}; }":{}}`),
	}))

	err := api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{"flatCallTracer":{}}`),
	})
	require.ErrorContains(t, err, "nested native debug tracer")
}
