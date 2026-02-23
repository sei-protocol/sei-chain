package ante_test

import (
	"testing"

	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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
	msgs      []sdk.Msg
	signers   []sdk.AccAddress
	body      *txtypes.TxBody
	authInfo  *txtypes.AuthInfo
	signature []signing.SignatureV2
}

func (tx mockTx) GetMsgs() []sdk.Msg                              { return tx.msgs }
func (tx mockTx) ValidateBasic() error                            { return nil }
func (tx mockTx) GetGasEstimate() uint64                          { return 0 }
func (tx mockTx) GetSigners() []sdk.AccAddress                    { return tx.signers }
func (tx mockTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (tx mockTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return tx.signature, nil }

// Add these methods for compatibility with no_cosmos_fields_test.go
func (tx mockTx) GetBody() *txtypes.TxBody {
	return tx.body
}
func (tx mockTx) GetAuthInfo() *txtypes.AuthInfo {
	return tx.authInfo
}

func TestRouter(t *testing.T) {
	bankMsg := &banktypes.MsgSend{}
	evmMsg, _ := evmtypes.NewMsgEVMTransaction(&ethtx.LegacyTx{})
	mockAnte := mockAnteState{}
	router := ante.NewEVMRouterDecorator(mockAnte.regularAnteHandler, mockAnte.evmAnteHandler)
	_, err := router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{bankMsg}}, false)
	require.Nil(t, err)
	require.Equal(t, "regular", mockAnte.call)
	_, err = router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{evmMsg}}, false)
	require.Nil(t, err)
	require.Equal(t, "evm", mockAnte.call)
	_, err = router.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{evmMsg, bankMsg}}, false)
	require.NotNil(t, err)
}
