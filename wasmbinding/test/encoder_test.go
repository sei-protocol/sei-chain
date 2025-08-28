package wasmbinding

import (
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
)

const (
	TEST_TARGET_CONTRACT = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"
	TEST_CREATOR         = "sei1y3pxq5dp900czh0mkudhjdqjq5m8cpmmps8yjw"
)

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
