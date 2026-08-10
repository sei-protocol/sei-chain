package evmrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

func TestRejectUnavailableHistoricalTrace(t *testing.T) {
	ctx, collector := withHistoricalTraceErrorCollector(context.Background())
	require.NotNil(t, historicalTraceErrorCollectorFromContext(ctx))

	historicalErr := errors.New("historical execution unavailable")
	collector.RecordHistoricalTraceError(historicalErr)

	result, err := rejectUnavailableHistoricalTrace("plausible but wrong", nil, collector)
	require.Nil(t, result)
	require.ErrorIs(t, err, historicalErr)
}

type recordingHistoricalTraceBlockTracer struct {
	err error
}

func (t recordingHistoricalTraceBlockTracer) TraceBlockByNumber(ctx context.Context, _ rpc.BlockNumber, _ *tracers.TraceConfig) ([]*tracers.TxTraceResult, error) {
	historicalTraceErrorCollectorFromContext(ctx).RecordHistoricalTraceError(t.err)
	return []*tracers.TxTraceResult{{}}, nil
}

func TestHistoricalTraceGuardedBlockTracer(t *testing.T) {
	historicalErr := errors.New("historical execution unavailable")
	tracer := historicalTraceGuardedBlockTracer{
		delegate: recordingHistoricalTraceBlockTracer{err: historicalErr},
	}

	result, err := tracer.TraceBlockByNumber(context.Background(), 1, nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, historicalErr)
}
