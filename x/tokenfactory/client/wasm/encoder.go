package wasm

import (
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/wasmbinding/bindings"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
)

func EncodeTokenFactoryCreateDenom(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedCreateDenomMsg := bindings.CreateDenom{}
	if err := json.Unmarshal(rawMsg, &encodedCreateDenomMsg); err != nil {
		return []sdk.Msg{}, types.ErrEncodeTokenFactoryCreateDenom
	}
	createDenomMsg := types.MsgCreateDenom{
		Sender:   sender.String(),
		Subdenom: encodedCreateDenomMsg.Subdenom,
	}
	return []sdk.Msg{&createDenomMsg}, nil
}

func EncodeTokenFactoryMint(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedMintMsg := bindings.MintTokens{}
	if err := json.Unmarshal(rawMsg, &encodedMintMsg); err != nil {
		return []sdk.Msg{}, types.ErrEncodeTokenFactoryMint
	}
	mintMsg := types.MsgMint{
		Sender: sender.String(),
		Amount: encodedMintMsg.Amount,
	}
	return []sdk.Msg{&mintMsg}, nil
}

func EncodeTokenFactoryBurn(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedBurnMsg := bindings.BurnTokens{}
	if err := json.Unmarshal(rawMsg, &encodedBurnMsg); err != nil {
		return []sdk.Msg{}, types.ErrEncodeTokenFactoryBurn
	}
	burnMsg := types.MsgBurn{
		Sender: sender.String(),
		Amount: encodedBurnMsg.Amount,
	}
	return []sdk.Msg{&burnMsg}, nil
}

func EncodeTokenFactoryChangeAdmin(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedChangeAdminMsg := bindings.ChangeAdmin{}
	if err := json.Unmarshal(rawMsg, &encodedChangeAdminMsg); err != nil {
		return []sdk.Msg{}, types.ErrEncodeTokenFactoryChangeAdmin
	}
	changeAdminMsg := types.MsgChangeAdmin{
		Sender:   sender.String(),
		Denom:    encodedChangeAdminMsg.Denom,
		NewAdmin: encodedChangeAdminMsg.NewAdminAddress,
	}
	return []sdk.Msg{&changeAdminMsg}, nil
}

func EncodeTokenFactorySetMetadata(rawMsg json.RawMessage, sender sdk.AccAddress) ([]sdk.Msg, error) {
	encodedSetMetadataMsg := bindings.SetMetadata{}
	if err := json.Unmarshal(rawMsg, &encodedSetMetadataMsg); err != nil {
		return []sdk.Msg{}, types.ErrEncodeTokenFactorySetMetadata
	}
	setMetadataMsg := types.MsgSetDenomMetadata{
		Sender:   sender.String(),
		Metadata: encodedSetMetadataMsg.Metadata,
	}
	return []sdk.Msg{&setMetadataMsg}, nil
}
