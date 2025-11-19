package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// ensure Msg interface compliance at compile time
var _ seitypes.Msg = &MsgVerifyInvariant{}

// NewMsgVerifyInvariant creates a new MsgVerifyInvariant object
//
//nolint:interfacer
func NewMsgVerifyInvariant(sender seitypes.AccAddress, invModeName, invRoute string) *MsgVerifyInvariant {
	return &MsgVerifyInvariant{
		Sender:              sender.String(),
		InvariantModuleName: invModeName,
		InvariantRoute:      invRoute,
	}
}

func (msg MsgVerifyInvariant) Route() string { return ModuleName }
func (msg MsgVerifyInvariant) Type() string  { return "verify_invariant" }

// get the bytes for the message signer to sign on
func (msg MsgVerifyInvariant) GetSigners() []seitypes.AccAddress {
	sender, _ := seitypes.AccAddressFromBech32(msg.Sender)
	return []seitypes.AccAddress{sender}
}

// GetSignBytes gets the sign bytes for the msg MsgVerifyInvariant
func (msg MsgVerifyInvariant) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(&msg)
	return sdk.MustSortJSON(bz)
}

// quick validity check
func (msg MsgVerifyInvariant) ValidateBasic() error {
	if msg.Sender == "" {
		return ErrNoSender
	}
	return nil
}

// FullInvariantRoute - get the messages full invariant route
func (msg MsgVerifyInvariant) FullInvariantRoute() string {
	return msg.InvariantModuleName + "/" + msg.InvariantRoute
}
