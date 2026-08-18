package evmrpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResultUnlessExpiredReportsTheDeadline(t *testing.T) {
	live := t.Context()

	traced := map[string]string{"gas": "0x1"}
	result, err := resultUnlessExpired(live, traced, nil)
	require.NoError(t, err)
	require.Equal(t, traced, result)

	underlying := errors.New("tracer failed")
	result, err = resultUnlessExpired(live, nil, underlying)
	require.ErrorIs(t, err, underlying)
	require.Nil(t, result)

	expired, cancelExpired := context.WithCancel(t.Context())
	cancelExpired()
	result, err = resultUnlessExpired(expired, map[string]string{"error": "store access cancelled"}, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
}
