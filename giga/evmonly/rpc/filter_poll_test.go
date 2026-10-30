package rpc

import (
	"context"
	"errors"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// pollFixture is a filterAPI over filterFixtureStore whose committed head the test moves.
type pollFixture struct {
	api   *filterAPI
	store *evmonly.MemoryReceiptStore
	head  uint64
}

func newPollFixture(t *testing.T) *pollFixture {
	t.Helper()
	fx := &pollFixture{store: filterFixtureStore(t), head: 9}
	backend := filterFixtureBackend(t, 0)
	backend.blockNumber = func() uint64 { return fx.head }
	backend.blockByHash = func(ctx context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
		return filterFixtureBackend(t, fx.head).BlockByHash(ctx, req)
	}
	fx.api = &filterAPI{
		backend:              backend,
		store:                fx.store,
		filters:              newFilterRegistry(time.Hour, 0),
		maxBlockFilterHashes: 5,
	}
	return fx
}

// commit indexes one contract A log with topic X at the next height and moves the head to it.
func (fx *pollFixture) commit(t *testing.T) uint64 {
	t.Helper()
	fx.head++
	hash := filterTxHash(fx.head, 0)
	require.NoError(t, fx.store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: hash,
		Receipt: &evmtypes.Receipt{TxHashHex: hash.Hex(), BlockNumber: fx.head, Logs: []*evmtypes.Log{
			{Address: filterContractA.Hex(), Topics: []string{filterTopicX.Hex()}},
		}},
	}}))
	require.NoError(t, fx.store.SetLatestVersion(int64(fx.head))) //nolint:gosec // test heights are small
	return fx.head
}

func logBlocks(logs []*ethtypes.Log) []uint64 {
	blocks := make([]uint64, len(logs))
	for i, lg := range logs {
		blocks[i] = lg.BlockNumber
	}
	return blocks
}

func TestNewFilterPollStart(t *testing.T) {
	blockHash := filterBlockHash(5)
	for _, tc := range []struct {
		name        string
		crit        filters.FilterCriteria
		pruneTo     uint64
		want        uint64
		wantPruned  bool
		wantInvalid bool
	}{
		{name: "no bounds start after the latest indexed block", crit: filters.FilterCriteria{}, want: 10},
		{name: "tag bounds start after the latest indexed block", crit: filters.FilterCriteria{
			FromBlock: big.NewInt(ethrpc.LatestBlockNumber.Int64()), ToBlock: big.NewInt(ethrpc.LatestBlockNumber.Int64()),
		}, want: 10},
		{name: "numeric fromBlock", crit: filters.FilterCriteria{FromBlock: big.NewInt(6)}, want: 6},
		{name: "earliest", crit: filters.FilterCriteria{FromBlock: big.NewInt(ethrpc.EarliestBlockNumber.Int64())}, want: 0},
		{name: "earliest after pruning", crit: filters.FilterCriteria{FromBlock: big.NewInt(0)}, pruneTo: 7, want: 0},
		{name: "future fromBlock", crit: filters.FilterCriteria{FromBlock: big.NewInt(50)}, want: 50},
		{name: "blockHash", crit: filters.FilterCriteria{BlockHash: &blockHash}, want: 0},
		{name: "pruned fromBlock", crit: filters.FilterCriteria{FromBlock: big.NewInt(5)}, pruneTo: 7, wantPruned: true},
		{name: "toBlock before start", crit: filters.FilterCriteria{FromBlock: big.NewInt(6), ToBlock: big.NewInt(5)}, wantInvalid: true},
		{name: "toBlock before the next block", crit: filters.FilterCriteria{ToBlock: big.NewInt(9)}, wantInvalid: true},
		{name: "fromBlock beyond int64", crit: filters.FilterCriteria{FromBlock: new(big.Int).Lsh(big.NewInt(1), 70)}, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newPollFixture(t)
			if tc.pruneTo > 0 {
				require.NoError(t, fx.store.PruneHistory(tc.pruneTo))
			}
			id, err := fx.api.NewFilter(t.Context(), tc.crit)
			if tc.wantPruned {
				require.ErrorIs(t, err, errLogRangePruned)
				return
			}
			if tc.wantInvalid {
				var invalid *invalidParamsError
				require.ErrorAs(t, err, &invalid)
				return
			}
			require.NoError(t, err)
			f, err := fx.api.filters.get(id)
			require.NoError(t, err)
			require.Equal(t, tc.want, f.cursor)
		})
	}
}

func TestGetFilterChangesPollsLogsFromCursor(t *testing.T) {
	fx := newPollFixture(t)
	poll := func(id ethrpc.ID) []*ethtypes.Log {
		t.Helper()
		got, err := fx.api.GetFilterChanges(t.Context(), id)
		require.NoError(t, err)
		return got.([]*ethtypes.Log)
	}
	history, err := fx.api.NewFilter(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(0), Addresses: []common.Address{filterContractA}})
	require.NoError(t, err)
	live, err := fx.api.NewFilter(t.Context(), filters.FilterCriteria{Topics: [][]common.Hash{{filterTopicX}}})
	require.NoError(t, err)
	bounded, err := fx.api.NewFilter(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(8), ToBlock: big.NewInt(10)})
	require.NoError(t, err)

	historyLogs := poll(history)
	require.Equal(t, []uint64{5, 5, 8}, logBlocks(historyLogs))
	require.Equal(t, filterBlockHash(5), historyLogs[0].BlockHash)
	require.Equal(t, uint(1), historyLogs[0].Index)
	require.Empty(t, poll(live))
	require.Equal(t, []uint64{8}, logBlocks(poll(bounded)))

	for _, id := range []ethrpc.ID{history, live, bounded} {
		require.NotNil(t, poll(id))
		require.Empty(t, poll(id))
	}

	fx.commit(t)
	fx.commit(t)
	require.Equal(t, []uint64{10, 11}, logBlocks(poll(history)))
	require.Equal(t, []uint64{10, 11}, logBlocks(poll(live)))
	require.Equal(t, []uint64{10}, logBlocks(poll(bounded)))
	fx.commit(t)
	require.Equal(t, []uint64{12}, logBlocks(poll(live)))
	require.Empty(t, poll(bounded))

	// eth_getFilterLogs resolves the criteria afresh and leaves the cursor alone.
	all, err := fx.api.GetFilterLogs(t.Context(), bounded)
	require.NoError(t, err)
	require.Equal(t, []uint64{8, 10}, logBlocks(all))
	fx.commit(t)
	require.Equal(t, []uint64{13}, logBlocks(poll(live)))
}

func TestGetFilterChangesBlockHashFilterPollsOnce(t *testing.T) {
	fx := newPollFixture(t)
	blockHash := filterBlockHash(5)
	id, err := fx.api.NewFilter(t.Context(), filters.FilterCriteria{BlockHash: &blockHash})
	require.NoError(t, err)

	for _, want := range [][]uint64{{5, 5, 5}, {}, {}} {
		got, err := fx.api.GetFilterChanges(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, want, logBlocks(got.([]*ethtypes.Log)))
	}
}

func TestGetFilterChangesReportsPrunedBlocksOnce(t *testing.T) {
	fx := newPollFixture(t)
	id, err := fx.api.NewFilter(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(5)})
	require.NoError(t, err)
	require.NoError(t, fx.store.PruneHistory(7))

	_, err = fx.api.GetFilterChanges(t.Context(), id)
	require.ErrorIs(t, err, errLogRangePruned)
	require.ErrorContains(t, err, "blocks 5 to 6")
	got, err := fx.api.GetFilterChanges(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []uint64{8}, logBlocks(got.([]*ethtypes.Log)))
}

func TestGetFilterChangesCapsEachPoll(t *testing.T) {
	for _, tc := range []struct {
		name      string
		logCounts map[uint64]int
		head      uint64
		// polls lists, per poll, the blocks its logs come from, or nil for a poll that fails.
		polls [][]uint64
	}{
		{
			name: "a poll covers at most maxBlocksForLogs blocks",
			logCounts: map[uint64]int{
				1: 1, maxBlocksForLogs: 1, maxBlocksForLogs + 1: 1,
			},
			head:  maxBlocksForLogs + 1,
			polls: [][]uint64{{1, maxBlocksForLogs}, {maxBlocksForLogs + 1}, {}},
		},
		{
			name:      "logs over the cap split the range",
			logCounts: map[uint64]int{1: maxLogsPerQuery / 2, 2: maxLogsPerQuery/2 + 1},
			head:      2,
			polls:     [][]uint64{{1}, {2}, {}},
		},
		{
			name:      "a block over the cap alone is skipped",
			logCounts: map[uint64]int{1: maxLogsPerQuery + 1, 2: 1},
			head:      2,
			polls:     [][]uint64{nil, {2}, {}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := evmonly.NewMemoryReceiptStore()
			for height, count := range tc.logCounts {
				logs := make([]*evmtypes.Log, count)
				for i := range logs {
					logs[i] = &evmtypes.Log{Address: filterContractA.Hex(), Index: uint32(i)} //nolint:gosec // small test count
				}
				hash := filterTxHash(height, 0)
				require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
					TxHash: hash, Receipt: &evmtypes.Receipt{TxHashHex: hash.Hex(), BlockNumber: height, Logs: logs},
				}}))
			}
			require.NoError(t, store.SetLatestVersion(int64(tc.head))) //nolint:gosec // test heights are small
			api := &filterAPI{backend: filterFixtureBackend(t, tc.head), store: store, filters: newFilterRegistry(0, 0)}
			id, err := api.NewFilter(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(1)})
			require.NoError(t, err)

			for i, want := range tc.polls {
				got, err := api.GetFilterChanges(t.Context(), id)
				if want == nil {
					require.ErrorIs(t, err, receipt.ErrTooManyLogs, "poll %d", i)
					continue
				}
				require.NoError(t, err, "poll %d", i)
				distinct := []uint64{}
				for _, block := range logBlocks(got.([]*ethtypes.Log)) {
					if len(distinct) == 0 || distinct[len(distinct)-1] != block {
						distinct = append(distinct, block)
					}
				}
				require.Equal(t, want, distinct, "poll %d", i)
			}
		})
	}
}

func TestGetFilterChangesPollsBlockHashes(t *testing.T) {
	fx := newPollFixture(t)
	id, err := fx.api.NewBlockFilter(t.Context())
	require.NoError(t, err)
	poll := func() []common.Hash {
		t.Helper()
		got, err := fx.api.GetFilterChanges(t.Context(), id)
		require.NoError(t, err)
		return got.([]common.Hash)
	}

	require.Empty(t, poll())
	fx.head += 2
	require.Equal(t, []common.Hash{filterBlockHash(10), filterBlockHash(11)}, poll())
	require.Empty(t, poll())
	fx.head++
	require.Equal(t, []common.Hash{filterBlockHash(12)}, poll())

	_, err = fx.api.GetFilterLogs(t.Context(), id)
	require.ErrorIs(t, err, errFilterNotFound)

	// A filter left behind by more than maxBlockFilterHashes blocks is removed.
	fx.head += fx.api.maxBlockFilterHashes + 1
	_, err = fx.api.GetFilterChanges(t.Context(), id)
	require.ErrorIs(t, err, errFilterNotFound)
	_, err = fx.api.GetFilterChanges(t.Context(), id)
	require.ErrorIs(t, err, errFilterNotFound)
}

func TestGetFilterChangesKeepsBlockHashesAfterAFailedLookup(t *testing.T) {
	fx := newPollFixture(t)
	backend := fx.api.backend.(*testBackend)
	serve := backend.block
	failAt := uint64(11)
	backend.block = func(ctx context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		if uint64(*req.Height) == failAt {
			return nil, errors.New("block store unavailable")
		}
		return serve(ctx, req)
	}
	id, err := fx.api.NewBlockFilter(t.Context())
	require.NoError(t, err)
	fx.head += 2

	// The lookup of block 11 fails after block 10's succeeded.
	_, err = fx.api.GetFilterChanges(t.Context(), id)
	require.Error(t, err)

	// The next poll returns both blocks.
	failAt = 0
	got, err := fx.api.GetFilterChanges(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{filterBlockHash(10), filterBlockHash(11)}, got)
}

func TestFilterRegistryLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		timeout    time.Duration
		maxFilters uint64
		wait       time.Duration
		// installs is how many filters the test installs before checking the first.
		installs  int
		wantFull  bool
		wantFound bool
	}{
		{name: "installed filter is found", timeout: time.Hour, installs: 1, wantFound: true},
		{name: "zero timeout never expires", installs: 1, wait: 20 * time.Millisecond, wantFound: true},
		{name: "unpolled filter expires", timeout: 10 * time.Millisecond, installs: 1, wait: 50 * time.Millisecond},
		{name: "cap refuses another filter", timeout: time.Hour, maxFilters: 2, installs: 3, wantFull: true, wantFound: true},
		{name: "expired filters free the cap", timeout: 10 * time.Millisecond, maxFilters: 1, installs: 1, wait: 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := newFilterRegistry(tc.timeout, tc.maxFilters)
			first, err := registry.add(&pollFilter{})
			require.NoError(t, err)
			for i := 1; i < tc.installs; i++ {
				_, err = registry.add(&pollFilter{})
			}
			if tc.wantFull {
				require.ErrorContains(t, err, "too many filters")
			} else {
				require.NoError(t, err)
			}
			time.Sleep(tc.wait)
			if tc.maxFilters > 0 && !tc.wantFull {
				_, err := registry.add(&pollFilter{})
				require.NoError(t, err)
			}

			_, err = registry.get(first)
			if !tc.wantFound {
				require.ErrorIs(t, err, errFilterNotFound)
				require.False(t, registry.remove(first))
				return
			}
			require.NoError(t, err)
			require.True(t, registry.remove(first))
			require.False(t, registry.remove(first))
			_, err = registry.get(first)
			require.ErrorIs(t, err, errFilterNotFound)
		})
	}
}

func TestFilterMethodsEndToEnd(t *testing.T) {
	fx := newPollFixture(t)
	limits := noRPCLimits
	limits.MaxFilters = 2
	handler, err := newHandler(fx.api.backend, fx.store, limits)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var logFilter, blockFilter string
	require.NoError(t, client.CallContext(t.Context(), &logFilter, "eth_newFilter", map[string]any{
		"fromBlock": "0x1", "address": filterContractA,
	}))
	require.NoError(t, client.CallContext(t.Context(), &blockFilter, "eth_newBlockFilter"))
	var refused string
	require.ErrorContains(t, client.CallContext(t.Context(), &refused, "eth_newBlockFilter"), "too many filters")

	var logs []*ethtypes.Log
	require.NoError(t, client.CallContext(t.Context(), &logs, "eth_getFilterChanges", logFilter))
	require.Equal(t, []uint64{5, 5, 8}, logBlocks(logs))
	require.NoError(t, client.CallContext(t.Context(), &logs, "eth_getFilterChanges", logFilter))
	require.NotNil(t, logs)
	require.Empty(t, logs)
	require.NoError(t, client.CallContext(t.Context(), &logs, "eth_getFilterLogs", logFilter))
	require.Equal(t, []uint64{5, 5, 8}, logBlocks(logs))

	fx.commit(t)
	var hashes []common.Hash
	require.NoError(t, client.CallContext(t.Context(), &hashes, "eth_getFilterChanges", blockFilter))
	require.Equal(t, []common.Hash{filterBlockHash(10)}, hashes)
	require.NoError(t, client.CallContext(t.Context(), &logs, "eth_getFilterChanges", logFilter))
	require.Equal(t, []uint64{10}, logBlocks(logs))

	var removed bool
	require.NoError(t, client.CallContext(t.Context(), &removed, "eth_uninstallFilter", blockFilter))
	require.True(t, removed)
	require.NoError(t, client.CallContext(t.Context(), &removed, "eth_uninstallFilter", blockFilter))
	require.False(t, removed)
	for _, method := range []string{"eth_getFilterChanges", "eth_getFilterLogs"} {
		require.ErrorContains(t, client.CallContext(t.Context(), &logs, method, blockFilter), errFilterNotFound.Error(), method)
	}

	var pending string
	err = client.CallContext(t.Context(), &pending, "eth_newPendingTransactionFilter")
	require.ErrorContains(t, err, "eth_newPendingTransactionFilter is not supported")
}
