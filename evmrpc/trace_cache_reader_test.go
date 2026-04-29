package evmrpc

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/stretchr/testify/require"
)

func TestBakeableTracerName(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name string
		cfg  *tracers.TraceConfig
		want string
	}{
		{"nil config (struct logger) — not bakeable", nil, ""},
		{"empty config (struct logger) — not bakeable", &tracers.TraceConfig{}, ""},
		{"callTracer plain — bakeable", &tracers.TraceConfig{Tracer: str("callTracer")}, "callTracer"},
		{"prestateTracer plain — bakeable", &tracers.TraceConfig{Tracer: str("prestateTracer")}, "prestateTracer"},
		{"flatCallTracer plain — bakeable", &tracers.TraceConfig{Tracer: str("flatCallTracer")}, "flatCallTracer"},
		{
			// TracerConfig isn't part of the cache key, so any custom config
			// makes the call un-bakeable — defensive against false hits.
			"callTracer with TracerConfig — not bakeable",
			&tracers.TraceConfig{Tracer: str("callTracer"), TracerConfig: json.RawMessage(`{"withLog":true}`)},
			"",
		},
		{"unknown named tracer — not bakeable", &tracers.TraceConfig{Tracer: str("noopTracer")}, ""},
		{"raw JS tracer — not bakeable", &tracers.TraceConfig{Tracer: str("function() { ... }")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, bakeableTracerName(tc.cfg))
		})
	}
}
