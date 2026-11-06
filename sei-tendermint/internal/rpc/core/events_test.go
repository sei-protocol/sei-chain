package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventlog"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func eventLogEnv(t *testing.T, numItems int) (*Environment, *eventlog.Log) {
	t.Helper()
	lg, err := eventlog.New(eventlog.LogSettings{WindowSize: time.Minute})
	require.NoError(t, err)
	for range numItems {
		require.NoError(t, lg.Add(types.EventNewBlockValue, types.EventDataString("x")))
	}
	return &Environment{EventLog: utils.Some(lg)}, lg
}

func TestEventsRejectsOversizedQuery(t *testing.T) {
	env, _ := eventLogEnv(t, 0)
	query := "tm.event MATCHES '" + strings.Repeat("a", maxQueryLength) + "'"

	_, err := env.Events(t.Context(), &coretypes.RequestEvents{
		Filter: &coretypes.EventFilter{Query: query},
	})
	require.Error(t, err)
}

func TestEventsScanStopsWhenContextEnds(t *testing.T) {
	env, lg := eventLogEnv(t, 3)
	req := &coretypes.RequestEvents{Before: lg.Info().Newest.String()}

	got, err := env.Events(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, got.Items, 2)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = env.Events(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}
