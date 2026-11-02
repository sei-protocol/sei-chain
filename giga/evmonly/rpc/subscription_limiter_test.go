package rpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionLimiter(t *testing.T) {
	type step struct {
		release    bool
		connection string
		wantErr    error
	}
	acquire := func(connection string) step { return step{connection: connection} }
	refuse := func(connection string, err error) step { return step{connection: connection, wantErr: err} }
	release := func(connection string) step { return step{release: true, connection: connection} }

	for _, tc := range []struct {
		name             string
		maxNewHeads      uint64
		maxPerConnection uint64
		steps            []step
	}{
		{
			name:             "per-connection cap refuses only that connection",
			maxPerConnection: 2,
			steps: []step{
				acquire("a"), acquire("a"), refuse("a", errConnectionSubscriptionLimit),
				acquire("b"), acquire("b"), refuse("b", errConnectionSubscriptionLimit),
			},
		},
		{
			name:        "node-wide cap spans connections",
			maxNewHeads: 3,
			steps: []step{
				acquire("a"), acquire("b"), acquire("c"),
				refuse("d", errNewHeadsSubscriptionLimit), refuse("a", errNewHeadsSubscriptionLimit),
			},
		},
		{
			name:             "node-wide cap is checked before the per-connection cap",
			maxNewHeads:      1,
			maxPerConnection: 1,
			steps:            []step{acquire("a"), refuse("a", errNewHeadsSubscriptionLimit)},
		},
		{
			name:             "release frees a per-connection slot",
			maxPerConnection: 1,
			steps:            []step{acquire("a"), refuse("a", errConnectionSubscriptionLimit), release("a"), acquire("a")},
		},
		{
			name:        "release frees a node-wide slot",
			maxNewHeads: 1,
			steps:       []step{acquire("a"), refuse("b", errNewHeadsSubscriptionLimit), release("a"), acquire("b")},
		},
		{
			name:  "zero caps disable both limits",
			steps: []step{acquire("a"), acquire("a"), acquire("a"), acquire("b")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limiter := newSubscriptionLimiter(tc.maxNewHeads, 0, tc.maxPerConnection)
			for i, s := range tc.steps {
				if s.release {
					limiter.releaseNewHeads(s.connection)
					continue
				}
				err := limiter.acquireNewHeads(s.connection)
				if s.wantErr != nil {
					require.ErrorIs(t, err, s.wantErr, "step %d", i)
				} else {
					require.NoError(t, err, "step %d", i)
				}
			}
		})
	}

	t.Run("releasing every slot forgets the connection", func(t *testing.T) {
		limiter := newSubscriptionLimiter(0, 0, 0)
		require.NoError(t, limiter.acquireNewHeads("a"))
		require.NoError(t, limiter.acquireNewHeads("a"))
		limiter.releaseNewHeads("a")
		require.Equal(t, map[string]uint64{"a": 1}, limiter.perConnection)
		limiter.releaseNewHeads("a")
		require.Empty(t, limiter.perConnection)
		require.Zero(t, limiter.newHeads)
	})
}

func TestSubscriptionLimiterCountsLogsSeparately(t *testing.T) {
	for _, tc := range []struct {
		name             string
		maxNewHeads      uint64
		maxLogs          uint64
		maxPerConnection uint64
		newHeads, logs   int
		wantErr          error
	}{
		{name: "logs cap refuses a logs subscription", maxNewHeads: 5, maxLogs: 2, logs: 2, wantErr: errLogsSubscriptionLimit},
		{name: "newHeads subscriptions leave the logs cap", maxNewHeads: 5, maxLogs: 2, newHeads: 5, logs: 1},
		{name: "per-connection cap spans both kinds", maxPerConnection: 3, newHeads: 2, logs: 1, wantErr: errConnectionSubscriptionLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limiter := newSubscriptionLimiter(tc.maxNewHeads, tc.maxLogs, tc.maxPerConnection)
			for range tc.newHeads {
				require.NoError(t, limiter.acquireNewHeads("a"))
			}
			for range tc.logs {
				require.NoError(t, limiter.acquireLogs("a"))
			}
			err := limiter.acquireLogs("a")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			limiter.releaseLogs("a")
			require.Equal(t, uint64(tc.logs), limiter.logs) //nolint:gosec // small test count
		})
	}
}
