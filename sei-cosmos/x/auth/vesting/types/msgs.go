package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// TypeMsgCreateVestingAccount defines the type value for a MsgCreateVestingAccount.
const TypeMsgCreateVestingAccount = "msg_create_vesting_account"

var _ seitypes.Msg = &MsgCreateVestingAccount{}

// NewMsgCreateVestingAccount returns a reference to a new MsgCreateVestingAccount.
//
//nolint:interfacer
func NewMsgCreateVestingAccount(fromAddr, toAddr seitypes.AccAddress, amount sdk.Coins, endTime int64, delayed bool, admin seitypes.AccAddress) *MsgCreateVestingAccount {
	return &MsgCreateVestingAccount{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		Amount:      amount,
		EndTime:     endTime,
		Delayed:     delayed,
		Admin:       admin.String(),
	}
}

// Route returns the message route for a MsgCreateVestingAccount.
func (msg MsgCreateVestingAccount) Route() string { return RouterKey }

// Type returns the message type for a MsgCreateVestingAccount.
func (msg MsgCreateVestingAccount) Type() string { return TypeMsgCreateVestingAccount }

// ValidateBasic Implements Msg.
func (msg MsgCreateVestingAccount) ValidateBasic() error {
	from, err := seitypes.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		return err
	}
	to, err := seitypes.AccAddressFromBech32(msg.ToAddress)
	if err != nil {
		return err
	}
	if err := seitypes.VerifyAddressFormat(from); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address: %s", err)
	}

	if err := seitypes.VerifyAddressFormat(to); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid recipient address: %s", err)
	}

	if !msg.Amount.IsValid() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, msg.Amount.String())
	}

	if !msg.Amount.IsAllPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, msg.Amount.String())
	}

	if msg.EndTime <= 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid end time")
	}

	return nil
}

// GetSignBytes returns the bytes all expected signers must sign over for a
// MsgCreateVestingAccount.
func (msg MsgCreateVestingAccount) GetSignBytes() []byte {
	return sdk.MustSortJSON(amino.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgCreateVestingAccount.
func (msg MsgCreateVestingAccount) GetSigners() []seitypes.AccAddress {
	from, err := seitypes.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{from}
}
