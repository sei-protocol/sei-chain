package wasm

import (
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func EncodeTokenFactoryCreateDenom(rawMsg json.RawMessage) ([]sdk.Msg, error) {
	createDenomMsg := types.MsgCreateDenom{}
	if err := json.Unmarshal(rawMsg, &createDenomMsg); err != nil {
		return []sdk.Msg{}, err
	}
	return []sdk.Msg{&createDenomMsg}, nil
}

func EncodeTokenFactoryMint(rawMsg json.RawMessage) ([]sdk.Msg, error) {
	mintMsg := types.MsgMint{}
	if err := json.Unmarshal(rawMsg, &mintMsg); err != nil {
		return []sdk.Msg{}, err
	}
	return []sdk.Msg{&mintMsg}, nil
}

func EncodeTokenFactoryBurn(rawMsg json.RawMessage) ([]sdk.Msg, error) {
	burnMsg := types.MsgBurn{}
	if err := json.Unmarshal(rawMsg, &burnMsg); err != nil {
		return []sdk.Msg{}, err
	}
	return []sdk.Msg{&burnMsg}, nil
}

func EncodeTokenFactoryChangeAdmin(rawMsg json.RawMessage) ([]sdk.Msg, error) {
	changeAdminMsg := types.MsgChangeAdmin{}
	if err := json.Unmarshal(rawMsg, &changeAdminMsg); err != nil {
		return []sdk.Msg{}, err
	}
	return []sdk.Msg{&changeAdminMsg}, nil
}
