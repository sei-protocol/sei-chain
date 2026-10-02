package evmrpc

import (
	"errors"
	"sync"
	"testing"
	"time"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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

func TestCtxAtHeightReturnsProviderContext(t *testing.T) {
	ctx, err := ctxAtHeight(func(h int64) sdk.Context { return sdk.Context{}.WithBlockHeight(h) }, 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), ctx.BlockHeight())
}
