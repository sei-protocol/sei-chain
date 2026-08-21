package app

import (
	"testing"

	"github.com/gogo/protobuf/proto"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	ibctransfertypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"
)

func TestRetiredTransferMessageRemainsDecodable(t *testing.T) {
	require.NotContains(t, ModuleBasics, ibctransfertypes.ModuleName)

	msg := &ibctransfertypes.MsgTransfer{}
	value, err := proto.Marshal(msg)
	require.NoError(t, err)

	any := &codectypes.Any{
		TypeUrl: sdk.MsgTypeURL(msg),
		Value:   value,
	}
	var decoded sdk.Msg
	require.NoError(t, MakeEncodingConfig().InterfaceRegistry.UnpackAny(any, &decoded))
	require.IsType(t, msg, decoded)
}
