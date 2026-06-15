package seiv3

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

func TestExecutorEmptyBlock(t *testing.T) {
	executor := NewExecutor(Config{OCCWorkers: 1})

	result, err := executor.ExecuteBlock(context.Background(), evmonly.BlockRequest{})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutorTxsRemainScaffoldOnly(t *testing.T) {
	executor := NewExecutor(Config{OCCWorkers: 1})

	_, err := executor.ExecuteBlock(context.Background(), evmonly.BlockRequest{
		Txs: [][]byte{{0x01}},
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, evmonly.ErrNotImplemented))
}
