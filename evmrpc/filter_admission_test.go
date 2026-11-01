package evmrpc

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

const (
	admissionMaxBlock = int64(2000)
	rateLimitedMsg    = "log query rate limit exceeded for large queries"
	tooBusyMsg        = "server too busy, rejecting new request"
)

// limiterWithTokens returns a limiter holding exactly n tokens that never refills.
func limiterWithTokens(n int) *rate.Limiter {
	return rate.NewLimiter(0, n)
}

// loadMetrics returns metrics with submitted tasks of which none completed, and the given queue
// capacity and DB-semaphore usage.
func loadMetrics(submitted int64, queueCap, semInUse, semCap int32) *WorkerPoolMetrics {
	m := &WorkerPoolMetrics{}
	m.TasksSubmitted.Store(submitted)
	m.QueueCapacity.Store(queueCap)
	m.DBSemaphoreAcquired.Store(semInUse)
	m.DBSemaphoreCapacity.Store(semCap)
	return m
}

func idleMetrics() *WorkerPoolMetrics { return loadMetrics(0, 100, 0, 20) }
func busyMetrics() *WorkerPoolMetrics { return loadMetrics(81, 100, 0, 20) }

// TestCheckLogQueryAdmission pins each admission rule on both sides of its threshold.
func TestCheckLogQueryAdmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		blockRange  int64
		tokens      int
		metrics     *WorkerPoolMetrics
		errContains string
	}{
		{name: "range at the rate-limit threshold skips the limiter", blockRange: RPSLimitThreshold, metrics: idleMetrics()},
		{name: "range above the threshold with a token is admitted", blockRange: RPSLimitThreshold + 1, tokens: 1, metrics: idleMetrics()},
		{name: "range above the threshold without a token is rate limited", blockRange: RPSLimitThreshold + 1, metrics: idleMetrics(), errContains: rateLimitedMsg},
		{name: "range at maxBlock is admitted", blockRange: admissionMaxBlock, tokens: 1, metrics: idleMetrics()},
		{name: "range over maxBlock is refused", blockRange: admissionMaxBlock + 1, tokens: 1, metrics: idleMetrics(), errContains: "block range too large (2001), maximum allowed is 2000 blocks"},
		{name: "queue at 80% is admitted", blockRange: 1, metrics: loadMetrics(80, 100, 0, 20)},
		{name: "queue over 80% is refused", blockRange: 1, metrics: busyMetrics(), errContains: tooBusyMsg},
		{name: "semaphore under 80% is admitted", blockRange: 1, metrics: loadMetrics(0, 100, 15, 20)},
		{name: "semaphore at 80% is refused", blockRange: 1, metrics: loadMetrics(0, 100, 16, 20), errContains: "server I/O saturated, rejecting new request"},
		{name: "an unknown semaphore capacity skips the I/O check", blockRange: 1, metrics: loadMetrics(0, 100, 50, 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkLogQueryAdmission(tc.blockRange, admissionMaxBlock, limiterWithTokens(tc.tokens), tc.metrics)
			if tc.errContains == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.errContains)
		})
	}
}

// TestCheckPollAdmission pins that a poll is cut to RPSLimitThreshold blocks rather than refused
// when no large-query token is left, keeps its whole range with one, and is refused only for its
// range or for load.
func TestCheckPollAdmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		blockRange  int64
		tokens      int
		metrics     *WorkerPoolMetrics
		want        int64
		errContains string
	}{
		{name: "a small poll needs no token", blockRange: 8, metrics: idleMetrics(), want: 8},
		{name: "a large poll with a token scans its whole range", blockRange: 500, tokens: 1, metrics: idleMetrics(), want: 500},
		{name: "a large poll without a token is cut to the threshold", blockRange: 500, metrics: idleMetrics(), want: RPSLimitThreshold},
		{name: "a poll over maxBlock is refused", blockRange: admissionMaxBlock + 1, tokens: 1, metrics: idleMetrics(), errContains: "block range too large"},
		{name: "a cut poll is still refused for load", blockRange: 500, metrics: busyMetrics(), errContains: tooBusyMsg},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := checkPollAdmission(tc.blockRange, admissionMaxBlock, limiterWithTokens(tc.tokens), tc.metrics)
			if tc.errContains != "" {
				require.ErrorContains(t, err, tc.errContains)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestCheckLogQueryAdmissionRangeFirst pins that an over-range query is refused before it can take
// a rate-limit token from the queries that are valid.
func TestCheckLogQueryAdmissionRangeFirst(t *testing.T) {
	t.Parallel()

	limiter := limiterWithTokens(1)
	err := checkLogQueryAdmission(admissionMaxBlock+1, admissionMaxBlock, limiter, idleMetrics())
	require.ErrorContains(t, err, "block range too large")
	require.True(t, limiter.Allow(), "the refused query must leave the token for the next one")
}

// TestLogQueryRefusalCodes pins the JSON-RPC codes clients see: a refusal for load or rate is
// -32005 (limit exceeded, retry later) and names no server internals; a range over the cap is the
// generic -32000, as before.
func TestLogQueryRefusalCodes(t *testing.T) {
	t.Parallel()

	shed := []struct {
		name    string
		blocks  int64
		metrics *WorkerPoolMetrics
	}{
		{name: "rate limited", blocks: RPSLimitThreshold + 1, metrics: idleMetrics()},
		{name: "queue over 80%", blocks: 1, metrics: busyMetrics()},
		{name: "semaphore at 80%", blocks: 1, metrics: loadMetrics(0, 100, 16, 20)},
	}
	for _, tc := range shed {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkLogQueryAdmission(tc.blocks, admissionMaxBlock, limiterWithTokens(0), tc.metrics)
			var coded ethrpc.Error
			require.ErrorAs(t, err, &coded)
			require.Equal(t, -32005, coded.ErrorCode())
			require.NotRegexp(t, `[0-9]`, err.Error(), "no queue depth or semaphore counts")
		})
	}

	err := checkLogQueryAdmission(admissionMaxBlock+1, admissionMaxBlock, limiterWithTokens(1), idleMetrics())
	var coded ethrpc.Error
	require.False(t, errors.As(err, &coded), "range too large keeps the default -32000")
}

// TestWithBounds pins that a tagged range is fixed to the numbers it was admitted at, and that a
// blockHash query is left alone.
func TestWithBounds(t *testing.T) {
	t.Parallel()

	pinned := withBounds(filters.FilterCriteria{FromBlock: big.NewInt(10), ToBlock: latestTag}, 10, 42)
	require.Equal(t, big.NewInt(10), pinned.FromBlock)
	require.Equal(t, big.NewInt(42), pinned.ToBlock)

	hash := common.HexToHash("0x01")
	byHash := withBounds(filters.FilterCriteria{BlockHash: &hash}, 10, 42)
	require.Nil(t, byHash.FromBlock)
	require.Nil(t, byHash.ToBlock)
}

const (
	admissionEarliest = int64(1)
	admissionHead     = int64(3000)
)

// emptyRangeReceiptStore answers every range query with no logs, so a read that passes admission
// completes and moves a filter's cursor.
type emptyRangeReceiptStore struct {
	fakeReceiptStore
}

func (*emptyRangeReceiptStore) FilterLogs(sdk.Context, uint64, uint64, filters.FilterCriteria, *receipt.LogBudget) ([]*ethtypes.Log, error) {
	return nil, nil
}

// newAdmissionTestAPI returns a FilterAPI over blocks admissionEarliest to admissionHead. Without
// k, a read that passes admission fails with receipt.ErrNotConfigured and one refused by it fails
// with the admission error, before any receipt is read.
func newAdmissionTestAPI(limiter *rate.Limiter, metrics *WorkerPoolMetrics, k *keeper.Keeper) *FilterAPI {
	cfg := &FilterConfig{timeout: time.Minute, maxLog: DefaultMaxLogLimit, maxBlock: admissionMaxBlock, maxFilters: DefaultMaxFilters}
	return &FilterAPI{
		filters:      make(map[ethrpc.ID]filter),
		filterConfig: cfg,
		logFetcher: &LogFetcher{
			k:            k,
			ctxProvider:  testCtxProvider,
			filterConfig: cfg,
			watermarks:   newHeightTestWatermarks(newHeightTestClient(0, admissionEarliest, admissionHead), admissionHead),
		},
		namespace:        "eth",
		shutdownCtx:      context.Background(),
		globalRPSLimiter: limiter,
		poolMetrics:      metrics,
	}
}

// lastBlocks is a numeric range covering the last n blocks up to admissionHead.
func lastBlocks(n int64) filters.FilterCriteria {
	return filters.FilterCriteria{FromBlock: big.NewInt(admissionHead - n + 1), ToBlock: big.NewInt(admissionHead)}
}

type logRead struct {
	name string
	read func(t *testing.T, api *FilterAPI, crit filters.FilterCriteria) error
}

var (
	getLogsRead = logRead{name: "eth_getLogs", read: func(t *testing.T, api *FilterAPI, crit filters.FilterCriteria) error {
		_, err := api.GetLogs(t.Context(), crit)
		return err
	}}
	getFilterLogsRead = logRead{name: "eth_getFilterLogs", read: func(t *testing.T, api *FilterAPI, crit filters.FilterCriteria) error {
		id, err := api.NewFilter(t.Context(), crit)
		require.NoError(t, err)
		_, err = api.GetFilterLogs(t.Context(), id)
		return err
	}}
	// filterPollRead reads a range through a new filter's first eth_getFilterChanges.
	filterPollRead = logRead{name: "eth_getFilterChanges", read: func(t *testing.T, api *FilterAPI, crit filters.FilterCriteria) error {
		id, err := api.NewFilter(t.Context(), crit)
		require.NoError(t, err)
		_, err = api.GetFilterChanges(t.Context(), id)
		return err
	}}
)

// requireRefusedWith asserts err is a load-shedding refusal that reaches go-ethereum's encoder
// unwrapped: rpc/json.go errorMessage takes the code by type assertion, so a wrapped refusal
// would go out as -32000.
func requireRefusedWith(t *testing.T, err error, msg string) {
	t.Helper()
	require.ErrorContains(t, err, msg)
	coded, ok := err.(ethrpc.Error) //nolint:errorlint // asserting the unwrapped form on purpose
	require.True(t, ok, "the method returns the refusal unwrapped")
	require.Equal(t, -32005, coded.ErrorCode())
}

// TestLogReadsShareAdmission pins that eth_getLogs, eth_getFilterLogs and eth_getFilterChanges
// apply the same admission: without a token a range over RPSLimitThreshold blocks is refused by
// the one-shot reads and cut by the poll, a smaller range is admitted by all three, and with the
// worker queue over 80% all three refuse even a small one, with code -32005 on the wire.
func TestLogReadsShareAdmission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		read    logRead
		cond    string
		blocks  int64
		metrics func() *WorkerPoolMetrics
		refused string
	}{
		{read: getLogsRead, cond: "50 blocks, no token", blocks: 50, metrics: idleMetrics},
		{read: getLogsRead, cond: "50 blocks, queue over 80%", blocks: 50, metrics: busyMetrics, refused: tooBusyMsg},
		{read: getLogsRead, cond: "500 blocks, no token", blocks: 500, metrics: idleMetrics, refused: rateLimitedMsg},
		{read: getFilterLogsRead, cond: "50 blocks, no token", blocks: 50, metrics: idleMetrics},
		{read: getFilterLogsRead, cond: "50 blocks, queue over 80%", blocks: 50, metrics: busyMetrics, refused: tooBusyMsg},
		{read: getFilterLogsRead, cond: "500 blocks, no token", blocks: 500, metrics: idleMetrics, refused: rateLimitedMsg},
		{read: filterPollRead, cond: "50 blocks, no token", blocks: 50, metrics: idleMetrics},
		{read: filterPollRead, cond: "50 blocks, queue over 80%", blocks: 50, metrics: busyMetrics, refused: tooBusyMsg},
		// Cut to RPSLimitThreshold blocks, not refused.
		{read: filterPollRead, cond: "500 blocks, no token", blocks: 500, metrics: idleMetrics},
	}
	for _, tc := range tests {
		t.Run(tc.read.name+"/"+tc.cond, func(t *testing.T) {
			t.Parallel()
			err := tc.read.read(t, newAdmissionTestAPI(limiterWithTokens(0), tc.metrics(), nil), lastBlocks(tc.blocks))
			if tc.refused != "" {
				requireRefusedWith(t, err, tc.refused)
				return
			}
			require.ErrorIs(t, err, receipt.ErrNotConfigured, "admitted, so the read reached the receipt store")
		})
	}
}

// TestFilterReadsSpendTheGetLogsBudget pins that the large-query tokens are one budget: a token a
// filter read takes is no longer there for eth_getLogs.
func TestFilterReadsSpendTheGetLogsBudget(t *testing.T) {
	t.Parallel()

	for _, r := range []logRead{getFilterLogsRead, filterPollRead} {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			api := newAdmissionTestAPI(limiterWithTokens(1), idleMetrics(), nil)
			require.ErrorIs(t, r.read(t, api, lastBlocks(500)), receipt.ErrNotConfigured, "the filter read takes the only token")
			_, err := api.GetLogs(t.Context(), lastBlocks(500))
			requireRefusedWith(t, err, rateLimitedMsg)
		})
	}
}

// TestRefusedPollKeepsCursor pins that a poll refused for load leaves the filter's cursor where it
// was, so the next poll retries the same blocks.
func TestRefusedPollKeepsCursor(t *testing.T) {
	t.Parallel()

	api := newAdmissionTestAPI(limiterWithTokens(0), busyMetrics(), nil)
	from := admissionHead - 49
	id, err := api.NewFilter(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(from)})
	require.NoError(t, err)

	_, err = api.GetFilterChanges(t.Context(), id)
	requireRefusedWith(t, err, tooBusyMsg)
	api.filtersMu.RLock()
	defer api.filtersMu.RUnlock()
	require.Equal(t, from, api.filters[id].logCursor)
}

// TestLaggingFilterCatchesUpWithoutTokens pins the liveness of a filter that has fallen behind:
// with no large-query token at all, every poll succeeds, scans RPSLimitThreshold blocks and moves
// the cursor that far, until the filter reaches the head, after which it has nothing to scan.
func TestLaggingFilterCatchesUpWithoutTokens(t *testing.T) {
	t.Parallel()

	k := &keeper.Keeper{}
	k.SetReceiptStoreForTesting(&emptyRangeReceiptStore{fakeReceiptStore{latest: admissionHead}})
	api := newAdmissionTestAPI(limiterWithTokens(0), idleMetrics(), k)
	const behind = 3 * RPSLimitThreshold
	from := admissionHead - behind + 1
	id, err := api.NewFilter(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(from)})
	require.NoError(t, err)

	cursor := func() int64 {
		api.filtersMu.RLock()
		defer api.filtersMu.RUnlock()
		return api.filters[id].logCursor
	}
	for poll := int64(1); poll <= behind/RPSLimitThreshold; poll++ {
		_, err := api.GetFilterChanges(t.Context(), id)
		require.NoError(t, err, "poll %d", poll)
		require.Equal(t, from+poll*RPSLimitThreshold, cursor(), "poll %d moves the cursor by RPSLimitThreshold", poll)
	}
	require.Equal(t, admissionHead+1, cursor(), "caught up with the head")

	_, err = api.GetFilterChanges(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, admissionHead+1, cursor(), "nothing left to scan")
}

// TestOverRangeFilterLogsKeepsToken pins that eth_getFilterLogs on a filter wider than maxBlock is
// refused for its range before it can take a rate-limit token, as eth_getLogs is.
func TestOverRangeFilterLogsKeepsToken(t *testing.T) {
	t.Parallel()

	limiter := limiterWithTokens(1)
	api := newAdmissionTestAPI(limiter, idleMetrics(), nil)
	id, err := api.NewFilter(t.Context(), lastBlocks(admissionMaxBlock+1))
	require.NoError(t, err, "eth_newFilter does not limit the range")

	_, err = api.GetFilterLogs(t.Context(), id)
	require.ErrorContains(t, err, "block range too large (2001), maximum allowed is 2000 blocks")
	require.True(t, limiter.Allow(), "the refused read must leave the token")
}
