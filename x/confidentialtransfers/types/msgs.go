package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// confidential transfers message types
const (
	TypeMsgTransfer            = "transfer"
	TypeMsgApplyPendingBalance = "apply_pending_balance"
	TypeMsgCloseAccount        = "close_account"
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

	if m.FromAmountLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "FromAmountLo is required")
	}

	if m.FromAmountHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "FromAmountHi is required")
	}

	if m.ToAmountLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "ToAmountLo is required")
	}

	if m.ToAmountHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "ToAmountHi is required")
	}

	if m.RemainingBalance == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RemainingBalance is required")
	}

	if m.Proofs == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Proofs is required")
	}

	err = m.Proofs.Validate()
	if err != nil {
		return err
	}

	return nil
}

func (m *MsgTransfer) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgTransfer) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}

func (m *MsgTransfer) FromProto() (*Transfer, error) {
	err := m.ValidateBasic()
	if err != nil {
		return nil, err
	}
	senderTransferAmountLo, err := m.FromAmountLo.FromProto()
	if err != nil {
		return nil, err
	}

	senderTransferAmountHi, err := m.FromAmountHi.FromProto()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountLo, err := m.ToAmountLo.FromProto()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountHi, err := m.ToAmountHi.FromProto()
	if err != nil {
		return nil, err
	}

	remainingBalanceCommitment, err := m.RemainingBalance.FromProto()
	if err != nil {
		return nil, err
	}

	proofs, err := m.Proofs.FromProto()
	if err != nil {
		return nil, err
	}

	// iterate over m.Auditors and convert them to types.Auditor
	auditors := make([]*TransferAuditor, 0, len(m.Auditors))
	for _, auditor := range m.Auditors {
		auditorData, e := auditor.FromProto()
		if e != nil {
			return nil, e
		}
		auditors = append(auditors, auditorData)
	}

	return &Transfer{
		FromAddress:                m.FromAddress,
		ToAddress:                  m.ToAddress,
		Denom:                      m.Denom,
		SenderTransferAmountLo:     senderTransferAmountLo,
		SenderTransferAmountHi:     senderTransferAmountHi,
		RecipientTransferAmountLo:  recipientTransferAmountLo,
		RecipientTransferAmountHi:  recipientTransferAmountHi,
		RemainingBalanceCommitment: remainingBalanceCommitment,
		DecryptableBalance:         m.DecryptableBalance,
		Proofs:                     proofs,
		Auditors:                   auditors,
	}, nil
}

var _ sdk.Msg = &MsgApplyPendingBalance{}

// Route Implements Msg.
func (m *MsgApplyPendingBalance) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgApplyPendingBalance) Type() string { return TypeMsgApplyPendingBalance }

func (m *MsgApplyPendingBalance) ValidateBasic() error {
	if len(m.Address) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Address is required")
	}

	if len(m.Denom) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Denom is required")
	}

	if len(m.NewDecryptableAvailableBalance) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "NewDecryptableAvailableBalance is required")
	}
	return nil
}

func (m *MsgApplyPendingBalance) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgApplyPendingBalance) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.Address)
	return []sdk.AccAddress{sender}
}

var _ sdk.Msg = &MsgCloseAccount{}

// Route Implements Msg.
func (m *MsgCloseAccount) Route() string { return RouterKey }

// Type Implements Msg.
func (m *MsgCloseAccount) Type() string { return TypeMsgCloseAccount }

func (m *MsgCloseAccount) ValidateBasic() error {
	if len(m.Address) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Address is required")
	}

	if len(m.Denom) == 0 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Denom is required")
	}

	if m.Proof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Proofs is required")
	}

	err := m.Proof.Validate()
	if err != nil {
		return err
	}

	return nil
}

func (m *MsgCloseAccount) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgCloseAccount) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.Address)
	return []sdk.AccAddress{sender}
}
