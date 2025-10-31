package evmrpc_test

import (
	"encoding/json"
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

func TestDebugAPIAsRawJSON(t *testing.T) {
	api := &evmrpc.DebugAPI{}

	t.Run("json RawMessage", func(t *testing.T) {
		msg := json.RawMessage(`{"hello":"world"}`)
		raw, ok := api.AsRawJSON(msg)
		require.True(t, ok)
		require.Equal(t, []byte(msg), raw)
	})

	t.Run("byte slice", func(t *testing.T) {
		input := []byte("bytes")
		raw, ok := api.AsRawJSON(input)
		require.True(t, ok)
		require.Equal(t, input, raw)
	})

	t.Run("string", func(t *testing.T) {
		input := "string"
		raw, ok := api.AsRawJSON(input)
		require.True(t, ok)
		require.Equal(t, []byte(input), raw)
	})

	t.Run("marshalable value", func(t *testing.T) {
		raw, ok := api.AsRawJSON(map[string]int{"answer": 42})
		require.True(t, ok)
		require.JSONEq(t, `{"answer":42}`, string(raw))
	})

	t.Run("marshal error", func(t *testing.T) {
		raw, ok := api.AsRawJSON(make(chan int))
		require.False(t, ok)
		require.Nil(t, raw)
	})
}
