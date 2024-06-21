package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgAssociate = "evm_associate"

var (
	_ sdk.Msg = &MsgAssociate{}
)

func NewMsgAssociate(sender sdk.AccAddress, customMsg string) *MsgAssociate {
	return &MsgAssociate{Sender: sender.String(), CustomMessage: customMsg}
}

func (msg *MsgAssociate) Route() string {
	return RouterKey
}

func (msg *MsgAssociate) Type() string {
	return TypeMsgAssociate
}

func (msg *MsgAssociate) GetSigners() []sdk.AccAddress {
	from, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{from}
}

func (msg *MsgAssociate) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

func (msg *MsgAssociate) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	return nil
}

func IsTxMsgAssociate(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}
	_, ok := msgs[0].(*MsgAssociate)
	return ok
}
