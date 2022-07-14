package wasm

import (
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

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
