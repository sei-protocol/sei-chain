package wasmbinding

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const TEST_TARGET_CONTRACT = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
const TEST_CREATOR = "sei1nc5tatafv6eyq7llkr2gv50ff9e22mnf70qgjlv737ktmt4eswrqms7u8a"

func TestDecodeOrder(t *testing.T) {
	order := types.Order{
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		PriceDenom:        "USDC",
		AssetDenom:        "SEI",
		Price:             sdk.MustNewDecFromStr("10"),
		Quantity:          sdk.OneDec(),
		Data:              "{\"position_effect\":\"OPEN\", \"leverage\":\"1\"}",
	}
	fund := sdk.NewCoin("usei", sdk.NewInt(1000000000))
	msg := types.MsgPlaceOrders{
		Creator:      TEST_CREATOR,
		Orders:       []*types.Order{&order},
		ContractAddr: TEST_TARGET_CONTRACT,
		Funds:        []sdk.Coin{fund},
	}
	serialized, _ := json.Marshal(msg)
	encodedMsg := base64.StdEncoding.EncodeToString(serialized)
	rawMsg := wasmbinding.RawMessage{
		MsgType: types.TypeMsgPlaceOrders,
		Data:    encodedMsg,
	}
	rawMsgs := wasmbinding.RawSdkMessages{Messages: []wasmbinding.RawMessage{rawMsg}}
	serializedRawMsgs, _ := json.Marshal(rawMsgs)
	encodedRawMsgs := base64.StdEncoding.EncodeToString(serializedRawMsgs)
	customMsg := wasmbinding.CustomMessage{Raw: encodedRawMsgs}
	serializedMsg, _ := json.Marshal(customMsg)

	decodedMsgs, err := wasmbinding.CustomEncoder(nil, serializedMsg)
	require.Nil(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*types.MsgPlaceOrders)
	require.True(t, ok)
	require.Equal(t, msg, *typedDecodedMsg)
}

func TestDecodeOrderCancellation(t *testing.T) {
	msg := types.MsgCancelOrders{
		Creator:      TEST_CREATOR,
		OrderIds:     []uint64{1},
		ContractAddr: TEST_TARGET_CONTRACT,
	}
	serialized, _ := json.Marshal(msg)
	encodedMsg := base64.StdEncoding.EncodeToString(serialized)
	rawMsg := wasmbinding.RawMessage{
		MsgType: types.TypeMsgCancelOrders,
		Data:    encodedMsg,
	}
	rawMsgs := wasmbinding.RawSdkMessages{Messages: []wasmbinding.RawMessage{rawMsg}}
	serializedRawMsgs, _ := json.Marshal(rawMsgs)
	encodedRawMsgs := base64.StdEncoding.EncodeToString(serializedRawMsgs)
	customMsg := wasmbinding.CustomMessage{Raw: encodedRawMsgs}
	serializedMsg, _ := json.Marshal(customMsg)

	decodedMsgs, err := wasmbinding.CustomEncoder(nil, serializedMsg)
	require.Nil(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*types.MsgCancelOrders)
	require.True(t, ok)
	require.Equal(t, msg, *typedDecodedMsg)
}
