package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	TypeMsgDepositToVault = "deposit_to_vault"
)

var _ sdk.Msg = &MsgDepositToVault{}

func NewMsgDepositToVault(depositor, amount string) *MsgDepositToVault {
	return &MsgDepositToVault{
		Depositor: depositor,
		Amount:    amount,
	}
}

func (msg MsgDepositToVault) Route() string { return RouterKey }

func (msg MsgDepositToVault) Type() string { return TypeMsgDepositToVault }

func (msg MsgDepositToVault) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Depositor); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid depositor address: %s", err)
	}

	if msg.Amount == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "amount cannot be empty")
	}

	if _, err := sdk.ParseCoinsNormalized(msg.Amount); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "invalid amount: %s", err)
	}

	return nil
}

func (msg MsgDepositToVault) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

func (msg MsgDepositToVault) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Depositor)
	return []sdk.AccAddress{addr}
}
