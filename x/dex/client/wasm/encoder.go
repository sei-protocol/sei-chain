package wasm

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type SeiDexWasmMessage struct {
	// queries the dex TWAPs
	PlaceOrders  json.RawMessage `json:"place_orders,omitempty"`
	CancelOrders json.RawMessage `json:"cancel_orders,omitempty"`
}

func EncodeDexMsg(msgData json.RawMessage) ([]sdk.Msg, error) {
	var parsedMessage SeiDexWasmMessage
	if err := json.Unmarshal(msgData, &parsedMessage); err != nil {
		return []sdk.Msg{}, sdkerrors.Wrap(err, "Error parsing Sei Dex Wasm Message")
	}
	switch {
	case parsedMessage.PlaceOrders != nil:
		return EncodeDexPlaceOrders(parsedMessage.PlaceOrders)
	case parsedMessage.CancelOrders != nil:
		return EncodeDexCancelOrders(parsedMessage.CancelOrders)
	default:
		return []sdk.Msg{}, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Dex Wasm Message"}
	}
}

func EncodeDexPlaceOrders(rawMsg json.RawMessage) ([]sdk.Msg, error) {
	placeOrdersMsg := types.MsgPlaceOrders{}
	if err := json.Unmarshal(rawMsg, &placeOrdersMsg); err != nil {
		return []sdk.Msg{}, err
	}
	return []sdk.Msg{&placeOrdersMsg}, nil
}

func EncodeDexCancelOrders(rawMsg json.RawMessage) ([]sdk.Msg, error) {
	cancelOrdersMsg := types.MsgCancelOrders{}
	if err := json.Unmarshal(rawMsg, &cancelOrdersMsg); err != nil {
		return []sdk.Msg{}, err
	}
	return []sdk.Msg{&cancelOrdersMsg}, nil
}
