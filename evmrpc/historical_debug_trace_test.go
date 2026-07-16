package evmrpc

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestIsHistoricalDebugTraceBlock(t *testing.T) {
	tests := []struct {
		name             string
		blockHeight      int64
		latestHeight     int64
		maxBlockLookback int64
		want             bool
	}{
		{
			name:             "older than configured lookback",
			blockHeight:      8,
			latestHeight:     10,
			maxBlockLookback: 1,
			want:             true,
		},
		{
			name:             "equal to configured lookback",
			blockHeight:      9,
			latestHeight:     10,
			maxBlockLookback: 1,
			want:             false,
		},
		{
			name:             "zero lookback treats previous block as historical",
			blockHeight:      9,
			latestHeight:     10,
			maxBlockLookback: 0,
			want:             true,
		},
		{
			name:             "zero lookback allows latest block",
			blockHeight:      10,
			latestHeight:     10,
			maxBlockLookback: 0,
			want:             false,
		},
		{
			name:             "negative lookback disables classification",
			blockHeight:      1,
			latestHeight:     10,
			maxBlockLookback: -1,
			want:             false,
		},
		{
			name:             "future block",
			blockHeight:      11,
			latestHeight:     10,
			maxBlockLookback: 0,
			want:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isHistoricalDebugTraceBlock(tt.blockHeight, tt.latestHeight, tt.maxBlockLookback))
		})
	}
}

func TestGuardHistoricalDebugTraceHeight(t *testing.T) {
	latestCtx := sdk.Context{}.WithBlockHeight(10)
	api := &DebugAPI{
		ctxProvider:      func(int64) sdk.Context { return latestCtx },
		connectionType:   ConnectionTypeHTTP,
		maxBlockLookback: -1,
	}

	err := api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 8)
	require.NoError(t, err)

	api.maxBlockLookback = 1
	err = api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 8)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block number 8 is beyond max lookback of 1")

	err = api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 9)
	require.NoError(t, err)

	api.maxBlockLookback = 0
	err = api.guardHistoricalDebugTraceHeight(context.Background(), "debug_traceBlockByNumber", 9)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block number 9 is beyond max lookback of 0")
}

func TestGuardHistoricalDebugTraceByHashUsesTendermintHeight(t *testing.T) {
	latestHeight := int64(10)
	latestCtx := sdk.Context{}.WithBlockHeight(latestHeight)
	tmClient := newHeightTestClient(8, 1, latestHeight)
	api := &DebugAPI{
		tmClient:         tmClient,
		ctxProvider:      func(int64) sdk.Context { return latestCtx },
		connectionType:   ConnectionTypeHTTP,
		maxBlockLookback: 1,
		backend: &Backend{
			watermarks: NewWatermarkManager(tmClient, func(int64) sdk.Context { return latestCtx }, nil, nil),
		},
	}

	err := api.guardHistoricalDebugTraceByHash(context.Background(), "debug_traceBlockByHash", common.HexToHash(highBlockHashHex))
	require.Error(t, err)
	require.Contains(t, err.Error(), "block number 8 is beyond max lookback of 1")

	err = api.guardHistoricalDebugTraceByHash(context.Background(), "debug_traceCall", common.HexToHash("0x1"))
	require.NoError(t, err)
}
