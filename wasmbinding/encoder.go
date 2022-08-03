package wasmbinding

import (
	"encoding/json"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	dexwasm "github.com/sei-protocol/sei-chain/x/dex/client/wasm"
	tokenfactorywasm "github.com/sei-protocol/sei-chain/x/tokenfactory/client/wasm"
)

type SeiWasmMessage struct {
	PlaceOrders  json.RawMessage `json:"place_orders,omitempty"`
	CancelOrders json.RawMessage `json:"cancel_orders,omitempty"`
	CreateDenom  json.RawMessage `json:"create_denom,omitempty"`
	Mint         json.RawMessage `json:"mint,omitempty"`
	Burn         json.RawMessage `json:"burn,omitempty"`
	ChangeAdmin  json.RawMessage `json:"change_admin,omitempty"`
}

func CustomEncoder(sender sdk.AccAddress, msg json.RawMessage) ([]sdk.Msg, error) {
	var parsedMessage SeiWasmMessage
	if err := json.Unmarshal(msg, &parsedMessage); err != nil {
		return []sdk.Msg{}, sdkerrors.Wrap(err, "Error parsing Sei Wasm Message")
	}
	switch {
	case parsedMessage.PlaceOrders != nil:
		return dexwasm.EncodeDexPlaceOrders(parsedMessage.PlaceOrders)
	case parsedMessage.CancelOrders != nil:
		return dexwasm.EncodeDexCancelOrders(parsedMessage.CancelOrders)
	case parsedMessage.CreateDenom != nil:
		return tokenfactorywasm.EncodeTokenFactoryCreateDenom(parsedMessage.CreateDenom, sender)
	case parsedMessage.Mint != nil:
		return tokenfactorywasm.EncodeTokenFactoryMint(parsedMessage.Mint, sender)
	case parsedMessage.Burn != nil:
		return tokenfactorywasm.EncodeTokenFactoryBurn(parsedMessage.Burn, sender)
	case parsedMessage.ChangeAdmin != nil:
		return tokenfactorywasm.EncodeTokenFactoryChangeAdmin(parsedMessage.ChangeAdmin, sender)
	default:
		return []sdk.Msg{}, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}
	}
}
