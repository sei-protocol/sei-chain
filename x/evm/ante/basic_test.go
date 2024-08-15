package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/params"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestBasicDecorator(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper()
	a := ante.NewBasicDecorator(k)
	msg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{})
	ctx, err := a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err) // expect out of gas err
	dataTooLarge := make([]byte, params.MaxInitCodeSize+1)
	for i := 0; i <= params.MaxInitCodeSize; i++ {
		dataTooLarge[i] = 1
	}
	msg, _ = types.NewMsgEVMTransaction(&ethtx.LegacyTx{Data: dataTooLarge})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "code size")
	negAmount := sdk.NewInt(-1)
	msg, _ = types.NewMsgEVMTransaction(&ethtx.LegacyTx{Amount: &negAmount})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Equal(t, sdkerrors.ErrInvalidCoins, err)
	data := make([]byte, 10)
	for i := 0; i < 10; i++ {
		dataTooLarge[i] = 1
	}
	msg, _ = types.NewMsgEVMTransaction(&ethtx.LegacyTx{Data: data})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Equal(t, sdkerrors.ErrOutOfGas, err)

	msg, _ = types.NewMsgEVMTransaction(&ethtx.BlobTx{GasLimit: 21000})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	require.Error(t, err, sdkerrors.ErrUnsupportedTxType)
}
