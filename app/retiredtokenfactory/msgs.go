// Package retiredtokenfactory provides decode-only compatibility for retired tokenfactory transactions.
package retiredtokenfactory

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

const (
	// ModuleName is the name of the retired tokenfactory module, which also names
	// its deleted store and its error codespace.
	ModuleName = "tokenfactory"

	TypeMsgCreateDenom      = "create_denom"
	TypeMsgUpdateDenom      = "update_denom"
	TypeMsgMint             = "mint"
	TypeMsgBurn             = "burn"
	TypeMsgChangeAdmin      = "change_admin"
	TypeMsgSetDenomMetadata = "set_denom_metadata"
)

var (
	_ sdk.Msg = (*MsgCreateDenom)(nil)
	_ sdk.Msg = (*MsgUpdateDenom)(nil)
	_ sdk.Msg = (*MsgMint)(nil)
	_ sdk.Msg = (*MsgBurn)(nil)
	_ sdk.Msg = (*MsgChangeAdmin)(nil)
	_ sdk.Msg = (*MsgSetDenomMetadata)(nil)
)

func (m MsgCreateDenom) Route() string { return ModuleName }

func (m MsgCreateDenom) Type() string { return TypeMsgCreateDenom }

func (m MsgCreateDenom) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgCreateDenom) GetSigners() []sdk.AccAddress { return senderSigners(m.Sender) }

func (m MsgCreateDenom) ValidateBasic() error { return ErrDeprecated }

func (m MsgUpdateDenom) Route() string { return ModuleName }

func (m MsgUpdateDenom) Type() string { return TypeMsgUpdateDenom }

func (m MsgUpdateDenom) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgUpdateDenom) GetSigners() []sdk.AccAddress { return senderSigners(m.Sender) }

func (m MsgUpdateDenom) ValidateBasic() error { return ErrDeprecated }

func (m MsgMint) Route() string { return ModuleName }

func (m MsgMint) Type() string { return TypeMsgMint }

func (m MsgMint) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgMint) GetSigners() []sdk.AccAddress { return senderSigners(m.Sender) }

func (m MsgMint) ValidateBasic() error { return ErrDeprecated }

func (m MsgBurn) Route() string { return ModuleName }

func (m MsgBurn) Type() string { return TypeMsgBurn }

func (m MsgBurn) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgBurn) GetSigners() []sdk.AccAddress { return senderSigners(m.Sender) }

func (m MsgBurn) ValidateBasic() error { return ErrDeprecated }

func (m MsgChangeAdmin) Route() string { return ModuleName }

func (m MsgChangeAdmin) Type() string { return TypeMsgChangeAdmin }

func (m MsgChangeAdmin) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgChangeAdmin) GetSigners() []sdk.AccAddress { return senderSigners(m.Sender) }

func (m MsgChangeAdmin) ValidateBasic() error { return ErrDeprecated }

func (m MsgSetDenomMetadata) Route() string { return ModuleName }

func (m MsgSetDenomMetadata) Type() string { return TypeMsgSetDenomMetadata }

func (m MsgSetDenomMetadata) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgSetDenomMetadata) GetSigners() []sdk.AccAddress { return senderSigners(m.Sender) }

func (m MsgSetDenomMetadata) ValidateBasic() error { return ErrDeprecated }

// senderSigners returns the sender as the only signer, or an empty address when
// the sender does not parse.
func senderSigners(sender string) []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(sender)
	return []sdk.AccAddress{addr}
}
