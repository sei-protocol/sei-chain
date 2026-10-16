package evmrpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/stretchr/testify/require"
)

func TestRunWithRecoveryHandlesPanic(t *testing.T) {
	var once sync.Once
	recovered := make(chan struct{})
	SetPanicHook(func(interface{}) {
		once.Do(func() { close(recovered) })
	})
	defer SetPanicHook(nil)

	runWithRecovery(func() {
		panic("should be handled")
	})

	select {
	case <-recovered:
	case <-time.After(time.Second):
		t.Fatal("expected panic to be recovered")
	}
}

func TestCtxAtHeightReturnsProviderPanicAsError(t *testing.T) {
	cause := errors.New("unable to load historical state with SS disabled for version: 7")
	_, err := ctxAtHeight(func(int64) sdk.Context { panic(cause) }, 7)
	require.ErrorIs(t, err, cause)
	require.Contains(t, err.Error(), "state at height 7 is unavailable")
}

func TestCtxAtHeightRepanicsNonErrorValues(t *testing.T) {
	require.PanicsWithValue(t, "boom", func() {
		_, _ = ctxAtHeight(func(int64) sdk.Context { panic("boom") }, 7)
	})
}

func TestCtxAtHeightRepanicsRuntimeErrors(t *testing.T) {
	require.Panics(t, func() {
		_, _ = ctxAtHeight(func(int64) sdk.Context {
			var heights []int64
			_ = heights[1]
			return sdk.Context{}
		}, 7)
	})
}

func TestCtxAtHeightReturnsProviderContext(t *testing.T) {
	ctx, err := ctxAtHeight(func(h int64) sdk.Context { return sdk.Context{}.WithBlockHeight(h) }, 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), ctx.BlockHeight())
}

func unavailableStateProvider(cause error) func(int64) sdk.Context {
	return func(h int64) sdk.Context {
		if h == LatestCtxHeight {
			return sdk.Context{}
		}
		panic(cause)
	}
}

func TestLogFetcherReturnsErrorWhenStateUnavailable(t *testing.T) {
	cause := errors.New("unable to load historical state with SS disabled")
	k := &keeper.Keeper{}
	k.SetReceiptStoreForTesting(&fakeReceiptStore{})
	f := &LogFetcher{
		k:                k,
		ctxProvider:      unavailableStateProvider(cause),
		filterConfig:     &FilterConfig{},
		dbReadSemaphore:  make(chan struct{}, 1),
		globalBlockCache: NewBlockCache(10),
	}

	_, err := f.tryFilterLogsRange(context.Background(), 5, 5, filters.FilterCriteria{}, 10)
	require.ErrorIs(t, err, cause)

	block := &coretypes.ResultBlock{Block: &tmtypes.Block{Header: tmtypes.Header{Height: 5}}}
	require.ErrorIs(t, f.collectLogs(context.Background(), block, filters.FilterCriteria{}, nil), cause)

	crit := filters.FilterCriteria{Addresses: []common.Address{{0x1}}}
	_, ok, err := f.readUncachedBlock(context.Background(), 5, crit, EncodeFilters(crit.Addresses, crit.Topics))
	require.ErrorIs(t, err, cause)
	require.False(t, ok)
}
