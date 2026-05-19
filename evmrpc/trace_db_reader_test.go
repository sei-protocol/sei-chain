package evmrpc

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
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
		// TracerConfig isn't part of the cache key, so any custom config
		// makes the call un-bakeable — guards against false cache hits.
		{
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

func TestBlockTraceCacheGet(t *testing.T) {
	c, err := keeper.NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	tx1 := common.HexToHash("0x11")
	tx2 := common.HexToHash("0x22")
	tx3 := common.HexToHash("0x33")
	str := func(s string) *string { return &s }
	cfg := &tracers.TraceConfig{Tracer: str("callTracer")}

	require.NoError(t, c.Put(5, "callTracer", tx1, json.RawMessage(`{"a":1}`)))
	require.NoError(t, c.Put(5, "callTracer", tx2, json.RawMessage(`{"a":2}`)))

	t.Run("all txs cached -> returns assembled list", func(t *testing.T) {
		got, ok := blockTraceCacheGet(c, 5, []common.Hash{tx1, tx2}, cfg)
		require.True(t, ok)
		require.Len(t, got, 2)
		require.Equal(t, tx1, got[0].TxHash)
		require.Equal(t, tx2, got[1].TxHash)
		require.JSONEq(t, `{"a":1}`, string(got[0].Result.(json.RawMessage)))
		require.JSONEq(t, `{"a":2}`, string(got[1].Result.(json.RawMessage)))
	})

	t.Run("any miss -> falls through", func(t *testing.T) {
		got, ok := blockTraceCacheGet(c, 5, []common.Hash{tx1, tx2, tx3}, cfg)
		require.False(t, ok, "any missing tx must report miss")
		require.Nil(t, got)
	})

	t.Run("nil cache -> miss", func(t *testing.T) {
		got, ok := blockTraceCacheGet(nil, 5, []common.Hash{tx1}, cfg)
		require.False(t, ok)
		require.Nil(t, got)
	})

	t.Run("unbakeable tracer config -> miss", func(t *testing.T) {
		got, ok := blockTraceCacheGet(c, 5, []common.Hash{tx1}, nil)
		require.False(t, ok)
		require.Nil(t, got)
	})

	t.Run("empty block -> empty hit", func(t *testing.T) {
		got, ok := blockTraceCacheGet(c, 5, []common.Hash{}, cfg)
		require.True(t, ok)
		require.Empty(t, got)
	})
}

func TestFilterExcludeFailFromBlockCache(t *testing.T) {
	c, err := keeper.NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	tx1 := common.HexToHash("0x11")
	tx2 := common.HexToHash("0x22")
	tx3 := common.HexToHash("0x33")

	// Per-block row mirrors what the baker writes: full TxTraceResult array
	// including entries with Error set.
	row, err := json.Marshal([]*tracers.TxTraceResult{
		{TxHash: tx1, Result: json.RawMessage(`{"a":1}`)},
		{TxHash: tx2, Error: "execution reverted"},
		{TxHash: tx3, Result: json.RawMessage(`{"a":3}`)},
	})
	require.NoError(t, err)
	require.NoError(t, c.PutBlock(9, "callTracer", row))

	t.Run("filters errored entries on hit", func(t *testing.T) {
		got, ok := filterExcludeFailFromBlockCache(c, 9, "callTracer", nil, sdk.Context{})
		require.True(t, ok)
		require.Len(t, got, 2)
		require.Equal(t, tx1, got[0].TxHash)
		require.Equal(t, tx3, got[1].TxHash)
	})

	t.Run("missing per-block row -> miss", func(t *testing.T) {
		_, ok := filterExcludeFailFromBlockCache(c, 99, "callTracer", nil, sdk.Context{})
		require.False(t, ok)
	})

	t.Run("malformed row -> miss", func(t *testing.T) {
		require.NoError(t, c.PutBlock(10, "callTracer", json.RawMessage(`not-json`)))
		_, ok := filterExcludeFailFromBlockCache(c, 10, "callTracer", nil, sdk.Context{})
		require.False(t, ok)
	})
}

func TestTryBlockResultCache(t *testing.T) {
	c, err := keeper.NewTraceDB(t.TempDir())
	require.NoError(t, err)
	defer c.Close()

	str := func(s string) *string { return &s }
	cfg := &tracers.TraceConfig{Tracer: str("callTracer")}
	require.NoError(t, c.PutBlock(7, "callTracer", json.RawMessage(`[{"x":1}]`)))

	t.Run("hit returns the raw block JSON", func(t *testing.T) {
		got, ok := tryBlockResultCache(c, 7, cfg)
		require.True(t, ok)
		require.JSONEq(t, `[{"x":1}]`, string(got.(json.RawMessage)))
	})

	t.Run("miss falls through", func(t *testing.T) {
		_, ok := tryBlockResultCache(c, 8, cfg)
		require.False(t, ok)
	})

	t.Run("unbakeable config -> miss", func(t *testing.T) {
		_, ok := tryBlockResultCache(c, 7, nil)
		require.False(t, ok)
	})

	t.Run("nil cache -> miss", func(t *testing.T) {
		_, ok := tryBlockResultCache(nil, 7, cfg)
		require.False(t, ok)
	})
}
