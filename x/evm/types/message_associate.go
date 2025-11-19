package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

const TypeMsgAssociate = "evm_associate"

var (
	_ seitypes.Msg = &MsgAssociate{}
)

func NewMsgAssociate(sender seitypes.AccAddress, customMsg string) *MsgAssociate {
	return &MsgAssociate{Sender: sender.String(), CustomMessage: customMsg}
}

func (msg *MsgAssociate) Route() string {
	return RouterKey
}

func (msg *MsgAssociate) Type() string {
	return TypeMsgAssociate
}

func (msg *MsgAssociate) GetSigners() []seitypes.AccAddress {
	from, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{from}
}

func (msg *MsgAssociate) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

func (msg *MsgAssociate) ValidateBasic() error {
	_, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}
	if len(msg.CustomMessage) > MaxAssociateCustomMessageLength {
		return sdkerrors.Wrapf(sdkerrors.ErrTxTooLarge, "custom message can have at most 64 characters")
	}

	return nil
}

func IsTxMsgAssociate(tx seitypes.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}
	_, ok := msgs[0].(*MsgAssociate)
	return ok
}
