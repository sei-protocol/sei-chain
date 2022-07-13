package wasmbinding

import (
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

const (
	TEST_TARGET_CONTRACT = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	TEST_CREATOR         = "sei1nc5tatafv6eyq7llkr2gv50ff9e22mnf70qgjlv737ktmt4eswrqms7u8a"
)

func TestEncodePlaceOrder(t *testing.T) {
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
	msgData := wasmbinding.SeiWasmMessage{
		PlaceOrders: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(nil, serializedMsg)
	require.NoError(t, err)
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
	msgData := wasmbinding.SeiWasmMessage{
		CancelOrders: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(nil, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*types.MsgCancelOrders)
	require.True(t, ok)
	require.Equal(t, msg, *typedDecodedMsg)
}
