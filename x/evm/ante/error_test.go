package ante_test

import (
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/example/code"
	abci "github.com/tendermint/tendermint/abci/types"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

type test struct {
	name         string
	tx           proto.Message
	simulate     bool
	ctxSetup     func(ctx sdk.Context) sdk.Context
	handlerErr   error
	txResultCode uint32
	assertions   func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error)
}

var testErr = errors.New("test")

func TestAnteErrorHandler_Handle(t *testing.T) {
	tests := []test{
		{
			name:         "no error should avoid appending an error",
			txResultCode: abci.CodeTypeOK,
			tx:           &ethtx.LegacyTx{},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 0)
			},
		},
		{
			name:         "error should append error to deferred info",
			handlerErr:   testErr,
			txResultCode: code.CodeTypeUnknownError,
			tx:           &ethtx.LegacyTx{},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.ErrorIs(t, err, testErr)
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 1)
				require.Equal(t, k.GetEVMTxDeferredInfo(ctx)[0].Error, testErr.Error())
			},
		},
		{
			name:         "error on check tx should avoid appending an error",
			txResultCode: code.CodeTypeUnknownError,
			ctxSetup: func(ctx sdk.Context) sdk.Context {
				return ctx.WithIsCheckTx(true)
			},
			handlerErr: testErr,
			tx:         &ethtx.LegacyTx{},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 0)
			},
		},
		{
			name:         "error on re-check tx should avoid appending an error",
			txResultCode: code.CodeTypeUnknownError,
			ctxSetup: func(ctx sdk.Context) sdk.Context {
				return ctx.WithIsReCheckTx(true)
			},
			handlerErr: testErr,
			tx:         &ethtx.LegacyTx{},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 0)
			},
		},
		{
			name:         "error with simulate should avoid appending an error",
			txResultCode: code.CodeTypeUnknownError,
			handlerErr:   testErr,
			simulate:     true,
			tx:           &ethtx.LegacyTx{},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 0)
			},
		},
		{
			name:         "error should not append error to deferred info if associate tx",
			handlerErr:   testErr,
			txResultCode: code.CodeTypeUnknownError,
			tx:           &ethtx.AssociateTx{},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.ErrorIs(t, err, testErr)
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 0)
			},
		},
		{
			name:         "error should not append error if data of tx cannot be decoded (not an evm message)",
			handlerErr:   testErr,
			txResultCode: code.CodeTypeUnknownError,
			tx: &sdk.GasInfo{ // not a valid eth tx, just a random proto so it will fail
				GasWanted: 100,
				GasUsed:   100,
			},
			assertions: func(t *testing.T, ctx sdk.Context, k *keeper.Keeper, err error) {
				require.Error(t, err, "failed to unpack message data")
				require.Len(t, k.GetEVMTxDeferredInfo(ctx), 0)
			},
		},
	}
	for _, test := range tests {
		k, ctx := testkeeper.MockEVMKeeper()
		if test.ctxSetup != nil {
			ctx = test.ctxSetup(ctx)
		}
		eh := ante.NewAnteErrorHandler(func(ctx sdk.Context, tx sdk.Tx, simulate bool) (newCtx sdk.Context, err error) {
			return ctx, test.handlerErr
		}, k)
		k.SetTxResults([]*abci.ExecTxResult{{Code: test.txResultCode}})
		msg, _ := types.NewMsgEVMTransaction(test.tx)
		newCtx, err := eh.Handle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, test.simulate)
		test.assertions(t, newCtx, k, err)
	}
}
