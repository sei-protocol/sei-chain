package wasmbinding

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
)

type CustomMessage struct {
	// specifies which module handler should handle the query
	Route string `json:"route,omitempty"`
	// The query data that should be parsed into the module query
	MessageData json.RawMessage `json:"message_data,omitempty"`
}

func CustomEncoder(sender sdk.AccAddress, msg json.RawMessage) ([]sdk.Msg, error) {
	customMsg := CustomMessage{}
	if err := json.Unmarshal(msg, &customMsg); err != nil {
		return []sdk.Msg{}, err
	}
	switch customMsg.Route {
	case DexRoute:
		return dexwasm.EncodeDexMsg(customMsg.MessageData)
	default:
		return []sdk.Msg{}, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Message Route"}
	}
}
