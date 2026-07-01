package types

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// TypeMsgCreateVestingAccount defines the type value for a MsgCreateVestingAccount.
const TypeMsgCreateVestingAccount = "msg_create_vesting_account"

// maxVestingCoinAmountBitLen bounds the magnitude of a single coin amount in
// MsgCreateVestingAccount. 2^200 is well above any realistic token supply.
const maxVestingCoinAmountBitLen = 200

var _ sdk.Msg = &MsgCreateVestingAccount{}

// NewMsgCreateVestingAccount returns a reference to a new MsgCreateVestingAccount.
func NewMsgCreateVestingAccount(fromAddr, toAddr sdk.AccAddress, amount sdk.Coins, endTime int64, delayed bool, admin sdk.AccAddress) *MsgCreateVestingAccount {
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

	if err := validateAddr(msg.FromAddress, "sender"); err != nil {
		return err
	}
	if err := validateAddr(msg.ToAddress, "recipient"); err != nil {
		return err
	}

	if !msg.Amount.IsValid() || !msg.Amount.IsAllPositive() {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidCoins, msg.Amount.String())
	}

	if msg.EndTime <= 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "invalid end time")
	}

	for _, c := range msg.Amount {
		if c.Amount.BigInt().BitLen() > maxVestingCoinAmountBitLen {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "%s amount is out of range", c.Denom)
		}
	}

	return nil
}

func validateAddr(bech32, label string) error {
	addr, err := sdk.AccAddressFromBech32(bech32)
	if err != nil {
		return err
	}
	if err := sdk.VerifyAddressFormat(addr); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid %s address: %s", label, err)
	}
	return nil
}

// GetSignBytes returns the bytes all expected signers must sign over for a
// MsgCreateVestingAccount.
func (msg MsgCreateVestingAccount) GetSignBytes() []byte {
	return sdk.MustSortJSON(amino.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgCreateVestingAccount.
func (msg MsgCreateVestingAccount) GetSigners() []sdk.AccAddress {
	from, err := sdk.AccAddressFromBech32(msg.FromAddress)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{from}
}
