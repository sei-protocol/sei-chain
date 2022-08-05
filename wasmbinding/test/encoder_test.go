package wasmbinding

import (
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
	"github.com/stretchr/testify/require"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

const (
	TEST_TARGET_CONTRACT = "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw"
	TEST_CREATOR         = "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw"
)

func TestEncodePlaceOrder(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	order := dextypes.Order{
		PositionDirection: dextypes.PositionDirection_LONG,
		OrderType:         dextypes.OrderType_LIMIT,
		PriceDenom:        "USDC",
		AssetDenom:        "SEI",
		Price:             sdk.MustNewDecFromStr("10"),
		Quantity:          sdk.OneDec(),
		Data:              "{\"position_effect\":\"OPEN\", \"leverage\":\"1\"}",
	}
	fund := sdk.NewCoin("usei", sdk.NewInt(1000000000))
	msg := bindings.PlaceOrders{
		Orders:       []*dextypes.Order{&order},
		Funds:        []sdk.Coin{fund},
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		PlaceOrders: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(contractAddr, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*dextypes.MsgPlaceOrders)
	require.True(t, ok)
	expectedMsg := dextypes.MsgPlaceOrders{
		Creator:      TEST_CREATOR,
		Orders:       []*dextypes.Order{&order},
		ContractAddr: TEST_TARGET_CONTRACT,
		Funds:        []sdk.Coin{fund},
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}

func TestDecodeOrderCancellation(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	msg := bindings.CancelOrders{
		OrderIds:     []uint64{1},
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		CancelOrders: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(contractAddr, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*dextypes.MsgCancelOrders)
	require.True(t, ok)
	expectedMsg := dextypes.MsgCancelOrders{
		Creator:      TEST_CREATOR,
		OrderIds:     []uint64{1},
		ContractAddr: TEST_TARGET_CONTRACT,
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}

func TestEncodeCreateDenom(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	msg := bindings.CreateDenom{
		Subdenom: "subdenom",
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		CreateDenom: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(contractAddr, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*tokenfactorytypes.MsgCreateDenom)
	require.True(t, ok)
	expectedMsg := tokenfactorytypes.MsgCreateDenom{
		Sender: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Subdenom: "subdenom",
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}

func TestEncodeMint(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	msg := bindings.MintTokens{
		Amount: sdk.Coin{Amount: sdk.NewInt(100), Denom: "subdenom"},
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		Mint: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(contractAddr, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*tokenfactorytypes.MsgMint)
	require.True(t, ok)
	expectedMsg := tokenfactorytypes.MsgMint{
		Sender: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Amount: sdk.Coin{Amount: sdk.NewInt(100), Denom: "subdenom"},
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}

func TestEncodeBurn(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	msg := bindings.BurnTokens{
		Amount: sdk.Coin{Amount: sdk.NewInt(10), Denom: "subdenom"},
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		Burn: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(contractAddr, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*tokenfactorytypes.MsgBurn)
	require.True(t, ok)
	expectedMsg := tokenfactorytypes.MsgBurn{
		Sender: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Amount: sdk.Coin{Amount: sdk.NewInt(10), Denom: "subdenom"},
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}

func TestEncodeChangeAdmin(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	msg := bindings.ChangeAdmin{
		Denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/subdenom",
		NewAdminAddress: "sei1hjfwcza3e3uzeznf3qthhakdr9juetl7g6esl4",
	}
	serialized, _ := json.Marshal(msg)
	msgData := wasmbinding.SeiWasmMessage{
		ChangeAdmin: serialized,
	}
	serializedMsg, _ := json.Marshal(msgData)

	decodedMsgs, err := wasmbinding.CustomEncoder(contractAddr, serializedMsg)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*tokenfactorytypes.MsgChangeAdmin)
	require.True(t, ok)
	expectedMsg := tokenfactorytypes.MsgChangeAdmin{
		Sender: "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Denom: "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/subdenom",
		NewAdmin: "sei1hjfwcza3e3uzeznf3qthhakdr9juetl7g6esl4",
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}
