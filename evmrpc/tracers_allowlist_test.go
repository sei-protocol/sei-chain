package evmrpc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/stretchr/testify/require"
)

// TestDefaultAllowedTracersAreNativeInGethRegistry pins the hardcoded native
// tracer list to the actual go-ethereum registry (whose init()s this package
// imports): every default-allowlisted name must be registered and non-JS, so a
// geth upgrade that changes the registry fails CI instead of silently
// weakening the JS-tracer gate.
func TestDefaultAllowedTracersAreNativeInGethRegistry(t *testing.T) {
	for _, name := range evmrpcconfig.DefaultTraceAllowedTracers() {
		require.False(t, tracers.DefaultDirectory.IsJS(name), "tracer %q must be registered as a native (non-JS) tracer in go-ethereum", name)
	}
}

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

	name = "  callTracer  "
	paddedCfg := &tracers.TraceConfig{Tracer: &name}
	require.NoError(t, api.validateTraceTracer(paddedCfg))
	require.Equal(t, callTracerName, *paddedCfg.Tracer, "validated name must be written back trimmed so geth resolves the native tracer")

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

	// Nested names are forwarded to geth inside the raw JSON, so a padded
	// native name cannot be rewritten and must be rejected outright.
	err = api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: json.RawMessage(`{" callTracer":{}}`),
	})
	require.ErrorContains(t, err, "must not have leading or trailing whitespace")
}

func TestValidateMuxTraceConfigRejectsDeepNesting(t *testing.T) {
	api := &DebugAPI{
		allowedTracers: buildAllowedTracerSet([]string{
			callTracerName,
			muxTracerName,
		}),
	}

	name := muxTracerName
	require.NoError(t, api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: nestedMuxTraceConfig(maxMuxTracerNestingDepth - 1),
	}))

	err := api.validateTraceTracer(&tracers.TraceConfig{
		Tracer:       &name,
		TracerConfig: nestedMuxTraceConfig(maxMuxTracerNestingDepth),
	})
	require.ErrorContains(t, err, "muxTracer nesting depth exceeds maximum")
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

func nestedMuxTraceConfig(nestedMuxTracers int) json.RawMessage {
	var builder strings.Builder
	for range nestedMuxTracers {
		builder.WriteString(`{"muxTracer":`)
	}
	builder.WriteString(`{"callTracer":{}}`)
	builder.WriteString(strings.Repeat("}", nestedMuxTracers))
	return json.RawMessage(builder.String())
}
