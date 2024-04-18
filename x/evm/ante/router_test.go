package ante_test

import (
	"testing"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
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

func (m *mockAnteState) regularAnteDepGenerator(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	m.call = "regulardep"
	return []sdkacltypes.AccessOperation{}, nil
}

func (m *mockAnteState) evmAnteDepGenerator(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	m.call = "evmdep"
	return []sdkacltypes.AccessOperation{}, nil
}

type mockTx struct {
	msgs    []sdk.Msg
	signers []sdk.AccAddress
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

func TestRouter(t *testing.T) {
	bankMsg := &banktypes.MsgSend{}
	evmMsg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{})
	mockAnte := mockAnteState{}
	router := ante.NewEVMRouterDecorator(mockAnte.regularAnteHandler, mockAnte.evmAnteHandler, mockAnte.regularAnteDepGenerator, mockAnte.evmAnteDepGenerator)
	_, err := router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{bankMsg}}, false)
	require.Nil(t, err)
	require.Equal(t, "regular", mockAnte.call)
	_, err = router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{evmMsg}}, false)
	require.Nil(t, err)
	require.Equal(t, "evm", mockAnte.call)
	_, err = router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{evmMsg, bankMsg}}, false)
	require.NotNil(t, err)
}

func TestEVMRouterDecorator_AnteDeps(t *testing.T) {
	bankMsg := &banktypes.MsgSend{}
	evmMsg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{})

	// non-EVM message
	mockAnte := mockAnteState{}
	router := ante.NewEVMRouterDecorator(mockAnte.regularAnteHandler, mockAnte.evmAnteHandler, mockAnte.regularAnteDepGenerator, mockAnte.evmAnteDepGenerator)
	txDeps := []sdkacltypes.AccessOperation{{}}
	_, err := router.AnteDeps(txDeps, mockTx{msgs: []sdk.Msg{bankMsg}}, 0)
	require.Nil(t, err)
	require.Equal(t, "regulardep", mockAnte.call)

	// EVM message
	mockAnte = mockAnteState{}
	_, err = router.AnteDeps(txDeps, mockTx{msgs: []sdk.Msg{evmMsg}}, 0)
	require.Nil(t, err)
	require.Equal(t, "evmdep", mockAnte.call)

	// mixed messages
	mockAnte = mockAnteState{}
	_, err = router.AnteDeps(txDeps, mockTx{msgs: []sdk.Msg{evmMsg, bankMsg}}, 0)
	require.NotNil(t, err)
	require.Equal(t, "", mockAnte.call)
}
