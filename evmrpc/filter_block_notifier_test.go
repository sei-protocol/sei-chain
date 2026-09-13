package evmrpc

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func newTestFilterAPI(t *testing.T, notifier *BlockHeaderNotifier) *FilterAPI {
	return newTestFilterAPIWithConfig(t, notifier, &FilterConfig{
		timeout:              time.Hour,
		maxLog:               1,
		maxLogBytes:          1,
		maxBlock:             1,
		maxFilters:           DefaultMaxFilters,
		maxBlockFilterHashes: DefaultMaxBlockFilterHashes,
	})
}

func newTestFilterAPIWithConfig(t *testing.T, notifier *BlockHeaderNotifier, filterConfig *FilterConfig) *FilterAPI {
	t.Helper()
	// Setup: FilterAPI with unused log/store deps; only the notifier path is exercised.
	api := NewFilterAPI(
		nil,
		nil,
		func(int64) sdk.Context { return sdk.Context{} },
		nil,
		filterConfig,
		ConnectionTypeHTTP,
		"eth",
		make(chan struct{}, 1),
		NewBlockCache(1),
		&sync.Mutex{},
		NewLogSlicePool(),
		nil,
		notifier,
	)
	t.Cleanup(api.shutdown)
	return api
}

func TestFilterAPI_RejectsFilterAboveCapacity(t *testing.T) {
	n := NewBlockHeaderNotifier(4)
	api := newTestFilterAPIWithConfig(t, n, &FilterConfig{timeout: time.Hour, maxFilters: 2})
	ctx := context.Background()

	_, err := api.NewFilter(ctx, filters.FilterCriteria{})
	require.NoError(t, err)
	_, err = api.NewBlockFilter(ctx)
	require.NoError(t, err)
	_, err = api.NewBlockFilter(ctx)
	require.EqualError(t, err, "too many filters: maximum allowed is 2")
}

func TestFilterAPI_InvalidatesBlockFilterWhenHashBacklogIsFull(t *testing.T) {
	n := NewBlockHeaderNotifier(4)
	api := newTestFilterAPIWithConfig(t, n, &FilterConfig{
		timeout:              time.Hour,
		maxFilters:           1,
		maxBlockFilterHashes: 2,
	})
	ctx := context.Background()
	id, err := api.NewBlockFilter(ctx)
	require.NoError(t, err)

	for height := int64(1); height <= 3; height++ {
		hash := common.BigToHash(big.NewInt(height))
		n.OnBlockCommitted(hash.Bytes(), &tmproto.Header{Height: height}, &abci.ResponseFinalizeBlock{})
	}

	_, err = api.GetFilterChanges(ctx, id)
	require.EqualError(t, err, "filter does not exist")
}

func TestFilterAPI_NewBlockFilterUsesNotifierHash(t *testing.T) {
	// Setup: FilterAPI subscribed to a private notifier.
	n := NewBlockHeaderNotifier(4)
	api := newTestFilterAPI(t, n)
	ctx := context.Background()

	// Setup: create a block filter before any commits.
	id, err := api.NewBlockFilter(ctx)
	require.NoError(t, err)

	// Test: poll before any commit.
	empty, err := api.GetFilterChanges(ctx, id)
	// Verify: empty slice, not nil.
	require.NoError(t, err)
	require.Equal(t, []common.Hash{}, empty)

	// Test: publish one Autobahn hash, then poll twice.
	hash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	n.OnBlockCommitted(hash.Bytes(), &tmproto.Header{Height: 9}, &abci.ResponseFinalizeBlock{})

	got, err := api.GetFilterChanges(ctx, id)
	// Verify: first poll returns that hash.
	require.NoError(t, err)
	require.Equal(t, []common.Hash{hash}, got)

	again, err := api.GetFilterChanges(ctx, id)
	// Verify: second poll is drained.
	require.NoError(t, err)
	require.Equal(t, []common.Hash{}, again)
}

func TestFilterAPI_NewBlockFilterAccumulatesUntilPoll(t *testing.T) {
	// Setup: FilterAPI with one live block filter.
	n := NewBlockHeaderNotifier(4)
	api := newTestFilterAPI(t, n)
	ctx := context.Background()
	id, err := api.NewBlockFilter(ctx)
	require.NoError(t, err)

	// Test: commit two blocks before the client polls.
	first := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	second := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	n.OnBlockCommitted(first.Bytes(), &tmproto.Header{Height: 1}, &abci.ResponseFinalizeBlock{})
	n.OnBlockCommitted(second.Bytes(), &tmproto.Header{Height: 2}, &abci.ResponseFinalizeBlock{})

	// Verify: one poll returns both hashes in order.
	got, err := api.GetFilterChanges(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{first, second}, got)
}

func TestFilterAPI_NewBlockFilterFanOutToEachFilter(t *testing.T) {
	// Setup: two live block filters on the same notifier.
	n := NewBlockHeaderNotifier(4)
	api := newTestFilterAPI(t, n)
	ctx := context.Background()
	idA, err := api.NewBlockFilter(ctx)
	require.NoError(t, err)
	idB, err := api.NewBlockFilter(ctx)
	require.NoError(t, err)

	// Test: publish one hash.
	hash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	n.OnBlockCommitted(hash.Bytes(), &tmproto.Header{Height: 3}, &abci.ResponseFinalizeBlock{})

	// Verify: each filter independently returns that hash.
	gotA, err := api.GetFilterChanges(ctx, idA)
	require.NoError(t, err)
	gotB, err := api.GetFilterChanges(ctx, idB)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{hash}, gotA)
	require.Equal(t, []common.Hash{hash}, gotB)
}

func TestFilterAPI_NewBlockFilterIgnoresBlocksBeforeCreate(t *testing.T) {
	// Setup: FilterAPI with a commit that happens before the filter exists.
	n := NewBlockHeaderNotifier(4)
	api := newTestFilterAPI(t, n)
	ctx := context.Background()
	before := common.HexToHash("0x0101010101010101010101010101010101010101010101010101010101010101")
	n.OnBlockCommitted(before.Bytes(), &tmproto.Header{Height: 1}, &abci.ResponseFinalizeBlock{})

	// Test: create the filter, then commit a later block.
	id, err := api.NewBlockFilter(ctx)
	require.NoError(t, err)
	after := common.HexToHash("0x0202020202020202020202020202020202020202020202020202020202020202")
	n.OnBlockCommitted(after.Bytes(), &tmproto.Header{Height: 2}, &abci.ResponseFinalizeBlock{})

	// Verify: only the post-create hash is returned.
	got, err := api.GetFilterChanges(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []common.Hash{after}, got)
}
