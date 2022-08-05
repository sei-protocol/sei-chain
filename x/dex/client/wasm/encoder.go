package wasm

import (
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
)

func EncodeDexPlaceOrders(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedPlaceOrdersMsg := bindings.PlaceOrders{}
	if err := json.Unmarshal(rawMsg, &encodedPlaceOrdersMsg); err != nil {
		return []sdk.Msg{}, err
	}
	placeOrdersMsg := types.MsgPlaceOrders{
		Creator: sender.String(),
		Orders: encodedPlaceOrdersMsg.Orders,
		ContractAddr: sender.String(),
		Funds: encodedPlaceOrdersMsg.Funds,
	}
	return []sdk.Msg{&placeOrdersMsg}, nil
}

func EncodeDexCancelOrders(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedCancelOrdersMsg := bindings.CancelOrders{}
	if err := json.Unmarshal(rawMsg, &encodedCancelOrdersMsg); err != nil {
		return []sdk.Msg{}, err
	}
	cancelOrdersMsg := types.MsgCancelOrders{
		Creator: sender.String(),
		OrderIds: encodedCancelOrdersMsg.OrderIds,
		ContractAddr: sender.String(),
	}
	return []sdk.Msg{&cancelOrdersMsg}, nil
}
