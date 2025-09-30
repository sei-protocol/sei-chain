package types

import (
	"encoding/hex"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	TypeMsgDepositToVault           = "deposit_to_vault"
	TypeMsgExecutePaywordSettlement = "execute_payword_settlement"
)

var (
	_ sdk.Msg = &MsgDepositToVault{}
	_ sdk.Msg = &MsgExecutePaywordSettlement{}
)

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

func NewMsgExecutePaywordSettlement(executor, recipient, payword, covenantHash, amount string) *MsgExecutePaywordSettlement {
	return &MsgExecutePaywordSettlement{
		Executor:     executor,
		Recipient:    recipient,
		Payword:      payword,
		CovenantHash: covenantHash,
		Amount:       amount,
	}
}

func (msg MsgExecutePaywordSettlement) Route() string { return RouterKey }

func (msg MsgExecutePaywordSettlement) Type() string {
	return TypeMsgExecutePaywordSettlement
}

func (msg MsgExecutePaywordSettlement) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Executor); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid executor address: %s", err)
	}

	if _, err := sdk.AccAddressFromBech32(msg.Recipient); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address: %s", err)
	}

	if strings.TrimSpace(msg.Payword) == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "payword cannot be empty")
	}

	if strings.TrimSpace(msg.CovenantHash) == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "covenant hash cannot be empty")
	}

	if _, err := NormalizeHexHash(msg.CovenantHash); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid covenant hash: %s", err)
	}

	if msg.Amount == "" {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, "amount cannot be empty")
	}

	if _, err := sdk.ParseCoinsNormalized(msg.Amount); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "invalid amount: %s", err)
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

func NormalizeHexHash(hash string) (string, error) {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(hash), "0x"))
	if _, err := hex.DecodeString(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}
