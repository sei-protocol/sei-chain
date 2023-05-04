package wasmbinding

import (
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
)

const (
	TEST_TARGET_CONTRACT = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
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
		Nominal:           sdk.ZeroDec(),
		TriggerPrice:      sdk.ZeroDec(),
		TriggerStatus:     false,
	}
	fund := sdk.NewCoin("usei", sdk.NewInt(1000000000))
	msg := bindings.PlaceOrders{
		Orders:       []*dextypes.Order{&order},
		Funds:        []sdk.Coin{fund},
		ContractAddr: TEST_TARGET_CONTRACT,
	}
	serializedMsg, _ := json.Marshal(msg)

	decodedMsgs, err := dexwasm.EncodeDexPlaceOrders(serializedMsg, contractAddr)
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
		Cancellations: []*types.Cancellation{
			{Id: 1},
		},
		ContractAddr: TEST_TARGET_CONTRACT,
	}
	serializedMsg, _ := json.Marshal(msg)

	decodedMsgs, err := dexwasm.EncodeDexCancelOrders(serializedMsg, contractAddr)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*dextypes.MsgCancelOrders)
	require.True(t, ok)
	expectedMsg := dextypes.MsgCancelOrders{
		Creator: TEST_CREATOR,
		Cancellations: []*types.Cancellation{
			{Id: 1, Price: sdk.ZeroDec()},
		},
		ContractAddr: TEST_TARGET_CONTRACT,
	}
	require.Equal(t, expectedMsg.Creator, typedDecodedMsg.Creator)
	require.Equal(t, *expectedMsg.Cancellations[0], *typedDecodedMsg.Cancellations[0])
	require.Equal(t, expectedMsg.ContractAddr, typedDecodedMsg.ContractAddr)
}

func TestEncodeCreateDenom(t *testing.T) {
	contractAddr, err := sdk.AccAddressFromBech32("sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw")
	require.NoError(t, err)
	msg := bindings.CreateDenom{
		Subdenom: "subdenom",
	}
	serializedMsg, _ := json.Marshal(msg)

	decodedMsgs, err := tokenfactorywasm.EncodeTokenFactoryCreateDenom(serializedMsg, contractAddr)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*tokenfactorytypes.MsgCreateDenom)
	require.True(t, ok)
	expectedMsg := tokenfactorytypes.MsgCreateDenom{
		Sender:   "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
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
	serializedMsg, _ := json.Marshal(msg)

	decodedMsgs, err := tokenfactorywasm.EncodeTokenFactoryMint(serializedMsg, contractAddr)
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
	serializedMsg, _ := json.Marshal(msg)

	decodedMsgs, err := tokenfactorywasm.EncodeTokenFactoryBurn(serializedMsg, contractAddr)
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
		Denom:           "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/subdenom",
		NewAdminAddress: "sei1hjfwcza3e3uzeznf3qthhakdr9juetl7g6esl4",
	}
	serializedMsg, _ := json.Marshal(msg)

	decodedMsgs, err := tokenfactorywasm.EncodeTokenFactoryChangeAdmin(serializedMsg, contractAddr)
	require.NoError(t, err)
	require.Equal(t, 1, len(decodedMsgs))
	typedDecodedMsg, ok := decodedMsgs[0].(*tokenfactorytypes.MsgChangeAdmin)
	require.True(t, ok)
	expectedMsg := tokenfactorytypes.MsgChangeAdmin{
		Sender:   "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw",
		Denom:    "factory/sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw/subdenom",
		NewAdmin: "sei1hjfwcza3e3uzeznf3qthhakdr9juetl7g6esl4",
	}
	require.Equal(t, expectedMsg, *typedDecodedMsg)
}
