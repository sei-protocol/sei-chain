package evmrpc

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

var errReceiptStoreDown = errors.New("receipt store down")

type failingReceiptStore struct{ fakeReceiptStore }

func (failingReceiptStore) GetReceipt(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
	return nil, errReceiptStoreDown
}

// A receipt lookup failure reaches the caller instead of "transaction not found".
func TestTraceTransactionSurfacesLookupError(t *testing.T) {
	t.Parallel()

	k := &keeper.Keeper{}
	k.SetReceiptStoreForTesting(&failingReceiptStore{})
	ctxProvider := func(int64) sdk.Context { return sdk.Context{} }
	api := &DebugAPI{
		tracersAPI:   tracers.NewAPI(newSlowIterTraceBackend(newSlowSkipStore())),
		keeper:       k,
		ctxProvider:  ctxProvider,
		backend:      &Backend{keeper: k, ctxProvider: ctxProvider},
		traceTimeout: time.Second,
	}

	_, err := api.TraceTransaction(t.Context(), common.HexToHash("0x01"), nil)
	require.ErrorIs(t, err, errReceiptStoreDown)
}
