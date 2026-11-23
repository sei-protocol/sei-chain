package evmrpc

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/stretchr/testify/require"
)

// pruningTestHeights are the earliest and latest block heights a pruningTestClient reports.
type pruningTestHeights struct {
	earliest, latest int64
}

// pruningTestClient is a heightTestClient whose earliest and latest heights move together, so a
// poll never sees one without the other.
type pruningTestClient struct {
	*heightTestClient
	heights atomic.Pointer[pruningTestHeights]
}

func (c *pruningTestClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	heights := c.heights.Load()
	return &coretypes.ResultStatus{
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHeight:   heights.latest,
			EarliestBlockHeight: heights.earliest,
		},
	}, nil
}

// TestSubscribeLogsEndsWhenPruningPassesItsCursor asserts that a logs subscription whose cursor
// pruning passes notifies an error naming the pruned blocks and ends.
func TestSubscribeLogsEndsWhenPruningPassesItsCursor(t *testing.T) {
	t.Parallel()
	tmClient := &pruningTestClient{heightTestClient: newHeightTestClient(0, 1, 100)}
	tmClient.heights.Store(&pruningTestHeights{earliest: 1, latest: 100})
	api := &SubscriptionAPI{
		logFetcher:        &LogFetcher{filterConfig: &FilterConfig{}, watermarks: newHeightTestWatermarks(tmClient, 1000)},
		subscriptonConfig: &SubscriptionConfig{logLimit: 1},
	}
	server := rpc.NewServer()
	require.NoError(t, server.RegisterName("eth", api))
	client := rpc.DialInProc(server)
	t.Cleanup(func() {
		client.Close()
		server.Stop()
	})

	// The subscription starts past the head, so a poll before pruning scans nothing.
	notifications := make(chan json.RawMessage, 4)
	sub, err := client.EthSubscribe(t.Context(), notifications, "logs", map[string]interface{}{"fromBlock": "0x6e"})
	require.NoError(t, err)
	t.Cleanup(sub.Unsubscribe)
	tmClient.heights.Store(&pruningTestHeights{earliest: 120, latest: 130})

	select {
	case raw := <-notifications:
		var notification map[string]string
		require.NoError(t, json.Unmarshal(raw, &notification), "notification %s", raw)
		require.Equal(t, map[string]string{"error": "logs of blocks 110 to 119 were pruned before the filter read them"}, notification)
	case <-time.After(2 * SleepInterval):
		t.Fatal("no notification for the pruned blocks")
	}
	require.Eventually(t, func() bool { return api.logSubsCount.Load() == 0 }, 2*SleepInterval, 10*time.Millisecond,
		"the subscription should end after notifying the pruned blocks")
}
