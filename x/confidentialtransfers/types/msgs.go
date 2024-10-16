package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// confidential transfers message types
const (
	TypeMsgTransfer = "transfer"
)

var _ sdk.Msg = &MsgTransfer{}

// Route Implements Msg.
func (m *MsgTransfer) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgTransfer) Type() string { return TypeMsgTransfer }

func (m *MsgTransfer) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.FromAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(m.ToAddress)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid recipient address (%s)", err)
	}

	err = sdk.ValidateDenom(m.Denom)
	if err != nil {
		return err
	}

	// TODO: Add the rest of the validation logic here
	return nil
}

func (m *MsgTransfer) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgTransfer) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}
