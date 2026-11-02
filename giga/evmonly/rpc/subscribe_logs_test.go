package rpc

import (
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// logsSubscriptionFixture serves a pollFixture over WebSocket, with an executed-blocks watch the
// test advances alongside the fixture's head.
type logsSubscriptionFixture struct {
	*pollFixture
	executed utils.AtomicSend[atypes.ExecutedBlocks]
	client   *ethclient.Client
}

func newLogsSubscriptionFixture(t *testing.T, limits gigaconfig.RPCConfig) *logsSubscriptionFixture {
	t.Helper()
	fx := &logsSubscriptionFixture{
		pollFixture: newPollFixture(t),
		executed:    utils.NewAtomicSend(atypes.NewExecutedBlocks(atypes.ExecutedBlock{Number: 9})),
	}
	backend := fx.api.backend.(*testBackend)
	backend.executedBlocks = func() (utils.AtomicRecv[atypes.ExecutedBlocks], error) {
		return fx.executed.Subscribe(), nil
	}
	handler, err := newHandler(backend, fx.store, limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(websocketHandler(handler))
	t.Cleanup(server.Close)
	fx.client, err = ethclient.DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"))
	require.NoError(t, err)
	t.Cleanup(fx.client.Close)
	return fx
}

// execute commits the next block and publishes it as executed.
func (fx *logsSubscriptionFixture) execute(t *testing.T) uint64 {
	t.Helper()
	height := fx.commit(t)
	fx.executed.Store(fx.executed.Load().Push(atypes.ExecutedBlock{Number: atypes.RoadIndex(height)}))
	return height
}

// receiveLogBlocks returns the blocks of the next n logs sub delivers.
func receiveLogBlocks(t *testing.T, sub ethereum.Subscription, logs <-chan ethtypes.Log, n int) []uint64 {
	t.Helper()
	blocks := make([]uint64, 0, n)
	for len(blocks) < n {
		select {
		case lg := <-logs:
			blocks = append(blocks, lg.BlockNumber)
		case err := <-sub.Err():
			t.Fatalf("subscription ended: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatalf("received %v, want %d logs", blocks, n)
		}
	}
	return blocks
}

func TestLogsSubscriptionStreamsMatchingLogs(t *testing.T) {
	blockHash := filterBlockHash(5)
	for _, tc := range []struct {
		name string
		crit ethereum.FilterQuery
		// history is the blocks of the logs delivered before any block executes.
		history []uint64
		live    bool
	}{
		{name: "open-ended query follows new blocks", crit: ethereum.FilterQuery{Addresses: []common.Address{filterContractA}}, live: true},
		{name: "numeric fromBlock replays history first", crit: ethereum.FilterQuery{
			FromBlock: big.NewInt(1), Addresses: []common.Address{filterContractA},
		}, history: []uint64{5, 5, 8}, live: true},
		{name: "numeric toBlock ends the stream", crit: ethereum.FilterQuery{
			FromBlock: big.NewInt(8), ToBlock: big.NewInt(9),
		}, history: []uint64{8}},
		{name: "blockHash delivers its block once", crit: ethereum.FilterQuery{BlockHash: &blockHash}, history: []uint64{5, 5, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newLogsSubscriptionFixture(t, noRPCLimits)
			logs := make(chan ethtypes.Log, 8)
			sub, err := fx.client.SubscribeFilterLogs(t.Context(), tc.crit, logs)
			require.NoError(t, err)
			t.Cleanup(sub.Unsubscribe)

			require.Equal(t, append([]uint64{}, tc.history...), receiveLogBlocks(t, sub, logs, len(tc.history)))
			height := fx.execute(t)
			if tc.live {
				require.Equal(t, []uint64{height}, receiveLogBlocks(t, sub, logs, 1))
				return
			}
			select {
			case lg := <-logs:
				t.Fatalf("finished subscription delivered a log from block %d", lg.BlockNumber)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}

func TestLogsSubscriptionWaitsForIndexedReceipts(t *testing.T) {
	fx := newLogsSubscriptionFixture(t, noRPCLimits)
	logs := make(chan ethtypes.Log, 1)
	sub, err := fx.client.SubscribeFilterLogs(t.Context(), ethereum.FilterQuery{}, logs)
	require.NoError(t, err)
	t.Cleanup(sub.Unsubscribe)

	// Block 10 executes before its receipts are indexed.
	fx.executed.Store(fx.executed.Load().Push(atypes.ExecutedBlock{Number: 10}))
	time.Sleep(2 * logsRetryInterval)
	fx.commit(t)
	require.Equal(t, []uint64{10}, receiveLogBlocks(t, sub, logs, 1))
}

func TestLogsSubscriptionRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		crit    ethereum.FilterQuery
		pruneTo uint64
		want    string
	}{
		{name: "logs cap", want: errLogsSubscriptionLimit.Error()},
		{name: "pruned fromBlock", crit: ethereum.FilterQuery{FromBlock: big.NewInt(5)}, pruneTo: 7, want: errLogRangePruned.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := noRPCLimits
			limits.MaxSubscriptionsLogs = 1
			fx := newLogsSubscriptionFixture(t, limits)
			first, err := fx.client.SubscribeFilterLogs(t.Context(), ethereum.FilterQuery{}, make(chan ethtypes.Log))
			require.NoError(t, err)
			if tc.pruneTo > 0 {
				first.Unsubscribe()
				require.NoError(t, fx.store.PruneHistory(tc.pruneTo))
			}
			// An unsubscribe frees its slot asynchronously.
			require.Eventually(t, func() bool {
				_, err = fx.client.SubscribeFilterLogs(t.Context(), tc.crit, make(chan ethtypes.Log))
				return err != nil && strings.Contains(err.Error(), tc.want)
			}, 5*time.Second, 10*time.Millisecond, "last error: %v", err)
		})
	}
}

func TestLogsSubscriptionFreesItsSlotWhenItsStreamEnds(t *testing.T) {
	blockHash := filterBlockHash(5)
	for _, tc := range []struct {
		name string
		crit ethereum.FilterQuery
	}{
		{name: "numeric toBlock", crit: ethereum.FilterQuery{FromBlock: big.NewInt(8), ToBlock: big.NewInt(9)}},
		{name: "blockHash", crit: ethereum.FilterQuery{BlockHash: &blockHash}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := noRPCLimits
			limits.MaxSubscriptionsLogs = 1
			fx := newLogsSubscriptionFixture(t, limits)
			finished, err := fx.client.SubscribeFilterLogs(t.Context(), tc.crit, make(chan ethtypes.Log, 8))
			require.NoError(t, err)
			t.Cleanup(finished.Unsubscribe)

			// The finished subscription is never unsubscribed, so only its stream ending frees the slot.
			require.Eventually(t, func() bool {
				sub, err := fx.client.SubscribeFilterLogs(t.Context(), ethereum.FilterQuery{}, make(chan ethtypes.Log))
				if err != nil {
					return false
				}
				sub.Unsubscribe()
				return true
			}, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestLogsSubscriptionRejectedOverPlainHTTP(t *testing.T) {
	fx := newPollFixture(t)
	handler, err := newHandler(fx.api.backend, fx.store, noRPCLimits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	_, err = client.EthSubscribe(t.Context(), make(chan ethtypes.Log), "logs", map[string]any{})
	require.ErrorIs(t, err, ethrpc.ErrNotificationsUnsupported)
}

func TestLogsErrorNotificationNamesASkippedBlock(t *testing.T) {
	skipped := hexutil.Uint64(12)
	for _, tc := range []struct {
		name string
		err  error
		want logsErrorNotification
	}{
		{name: "oversized block", err: &oversizedBlockError{height: 12, err: errLogRangeTooWide},
			want: logsErrorNotification{Error: (&oversizedBlockError{height: 12, err: errLogRangeTooWide}).Error(), BlockNumber: &skipped}},
		{name: "other failure", err: errLogRangePruned, want: logsErrorNotification{Error: errLogRangePruned.Error()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, newLogsErrorNotification(tc.err))
		})
	}
}
