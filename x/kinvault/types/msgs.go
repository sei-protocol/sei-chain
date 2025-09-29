package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	ModuleName = "kinvault"
	RouterKey  = ModuleName

	TypeMsgWithdrawWithSigil = "WithdrawWithSigil"
)

type MsgWithdrawWithSigil struct {
	Sender       string `json:"sender"`
	VaultId      string `json:"vault_id"`
	KinProof     string `json:"kin_proof"`
	HoloPresence string `json:"holo_presence"`
}

var _ sdk.Msg = &MsgWithdrawWithSigil{}

func NewMsgWithdrawWithSigil(sender sdk.AccAddress, vaultID, kinProof, holoPresence string) *MsgWithdrawWithSigil {
	return &MsgWithdrawWithSigil{
		Sender:       sender.String(),
		VaultId:      vaultID,
		KinProof:     kinProof,
		HoloPresence: holoPresence,
	}
}

func (msg MsgWithdrawWithSigil) Route() string { return RouterKey }

func (msg MsgWithdrawWithSigil) Type() string { return TypeMsgWithdrawWithSigil }

func (msg MsgWithdrawWithSigil) GetSigners() []sdk.AccAddress {
	addr, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{addr}
}

func (msg MsgWithdrawWithSigil) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&msg))
}

func (msg MsgWithdrawWithSigil) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Sender); err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid sender address (%s)", err)
	}

	if len(msg.VaultId) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "vault id cannot be empty")
	}

	if len(msg.KinProof) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "kin proof cannot be empty")
	}

	if len(msg.HoloPresence) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "holo presence cannot be empty")
	}

	return nil
}
