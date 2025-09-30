package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	TypeMsgDepositToVault           = "deposit_to_vault"
	TypeMsgExecutePaywordSettlement = "execute_payword_settlement"
)

var _ sdk.Msg = &MsgDepositToVault{}
var _ sdk.Msg = &MsgExecutePaywordSettlement{}

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

func NewMsgExecutePaywordSettlement(executor, covenantID, payee, amount string) *MsgExecutePaywordSettlement {
	return &MsgExecutePaywordSettlement{
		Executor:   executor,
		CovenantId: covenantID,
		Payee:      payee,
		Amount:     amount,
	}
}

func (msg MsgExecutePaywordSettlement) Route() string { return RouterKey }

func (msg MsgExecutePaywordSettlement) Type() string { return TypeMsgExecutePaywordSettlement }

func (msg MsgExecutePaywordSettlement) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Executor); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid executor address: %s", err)
	}

	if msg.CovenantId == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "covenant id cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(msg.Payee); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid payee address: %s", err)
	}

	if msg.Amount == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "amount cannot be empty")
	}

	coins, err := sdk.ParseCoinsNormalized(msg.Amount)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "invalid amount: %s", err)
	}

	if !coins.IsAllPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "amount must be positive")
	}

	return nil
}

func (msg MsgExecutePaywordSettlement) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

func (msg MsgExecutePaywordSettlement) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Executor)
	return []sdk.AccAddress{addr}
}
