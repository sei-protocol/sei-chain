package antedecorators_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestAuthzNestedEvmMessage(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	output = ""
	anteDecorators := []sdk.AnteDecorator{
		antedecorators.NewAuthzNestedMessageDecorator(),
	}
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)
	chainedHandler := sdk.ChainAnteDecorators(anteDecorators...)

	nestedEvmMessage := authz.NewMsgExec(addr1, []sdk.Msg{&evmtypes.MsgEVMTransaction{}})
	// test with nested evm message
	_, err := chainedHandler(
		ctx.WithPriority(0),
		FakeTx{
			FakeMsgs: []sdk.Msg{&nestedEvmMessage},
		},
		false,
	)
	require.NotNil(t, err)

	// Multiple nested layers to evm message
	doubleNestedEvmMessage := authz.NewMsgExec(addr1, []sdk.Msg{&nestedEvmMessage})
	_, err = chainedHandler(
		ctx.WithPriority(0),
		FakeTx{
			FakeMsgs: []sdk.Msg{&doubleNestedEvmMessage},
		},
		false,
	)
	require.NotNil(t, err)

	// No error
	nestedMessage := authz.NewMsgExec(addr1, []sdk.Msg{&banktypes.MsgSend{}})
	_, err = chainedHandler(
		ctx.WithPriority(0),
		FakeTx{
			FakeMsgs: []sdk.Msg{&nestedMessage},
		},
		false,
	)
	require.Nil(t, err)
}
