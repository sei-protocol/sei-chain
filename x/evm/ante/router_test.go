package ante

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

type mockAnteState struct {
	call string
}

func (m *mockAnteState) regularAnteHandler(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	m.call = "regular"
	return ctx, nil
}

func (m *mockAnteState) evmAnteHandler(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	m.call = "evm"
	return ctx, nil
}

type mockTx struct {
	msgs []sdk.Msg
}

func (tx mockTx) GetMsgs() []sdk.Msg   { return tx.msgs }
func (tx mockTx) ValidateBasic() error { return nil }

func TestRouter(t *testing.T) {
	bankMsg := &banktypes.MsgSend{}
	evmMsg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{})
	mockAnte := mockAnteState{}
	router := NewEVMRouterDecorator(mockAnte.regularAnteHandler, mockAnte.evmAnteHandler)
	_, err := router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{bankMsg}}, false)
	require.Nil(t, err)
	require.Equal(t, "regular", mockAnte.call)
	_, err = router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{evmMsg}}, false)
	require.Nil(t, err)
	require.Equal(t, "evm", mockAnte.call)
	_, err = router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{evmMsg, bankMsg}}, false)
	require.NotNil(t, err)
}
