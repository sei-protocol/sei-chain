package evmrpc_test

import (
	"encoding/json"
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestTraceTendermintIfApplicable(t *testing.T) {
	unencodable := make(chan int)
	require.NotPanics(t, func() {
		evmrpc.TraceTendermintIfApplicable(t.Context(), "Block", []string{"1"}, unencodable)
	}, "response must not be encoded when tracing is off")

	traces := &evmrpc.TendermintTraces{}
	ctx := evmrpc.WithTendermintTraces(t.Context(), traces)
	evmrpc.TraceTendermintIfApplicable(ctx, "Block", []string{"1"}, map[string]int{"height": 1})
	require.Equal(t, []evmrpc.TendermintTrace{{
		Endpoint:  "Block",
		Arguments: []string{"1"},
		Response:  json.RawMessage(`{"height":1}`),
	}}, traces.Traces)
}
