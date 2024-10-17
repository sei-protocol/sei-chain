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

	transfer, err := m.ToTransfer()
	if err != nil {
		return err
	}

	if transfer.SenderTransferAmountLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "SenderTransferAmountLo is required")
	}

	if transfer.SenderTransferAmountHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "SenderTransferAmountHi is required")
	}

	if transfer.RecipientTransferAmountLo == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RecipientTransferAmountLo is required")
	}

	if transfer.RecipientTransferAmountHi == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RecipientTransferAmountHi is required")
	}

	if transfer.RemainingBalanceCommitment == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RemainingBalanceCommitment is required")
	}

	if transfer.Proofs == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "Proofs is required")
	}

	if transfer.Proofs.RemainingBalanceCommitmentValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RemainingBalanceCommitmentValidityProof is required")
	}

	if transfer.Proofs.SenderTransferAmountLoValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "SenderTransferAmountLoValidityProof is required")
	}

	if transfer.Proofs.SenderTransferAmountHiValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "SenderTransferAmountHiValidityProof is required")
	}

	if transfer.Proofs.RecipientTransferAmountLoValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RecipientTransferAmountLoValidityProof is required")
	}

	if transfer.Proofs.RecipientTransferAmountHiValidityProof == nil {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RecipientTransferAmountHiValidityProof is required")
	}

	//if transfer.Proofs.RemainingBalanceRangeProof == nil {
	//	return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RemainingBalanceRangeProof is required")
	//}
	//
	//if transfer.Proofs.RemainingBalanceEqualityProof == nil {
	//	return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "RemainingBalanceEqualityProof is required")
	//}
	//
	//if transfer.Proofs.TransferAmountLoEqualityProof == nil {
	//	return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "TransferAmountLoEqualityProof is required")
	//}
	//
	//if transfer.Proofs.TransferAmountHiEqualityProof == nil {
	//	return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "TransferAmountHiEqualityProof is required")
	//}
	return nil
}

func (m *MsgTransfer) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

func (m *MsgTransfer) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.FromAddress)
	return []sdk.AccAddress{sender}
}

func (m *MsgTransfer) ToTransfer() (*Transfer, error) {
	senderTransferAmountLo, err := m.ToAmountHi.ToCiphertext()
	if err != nil {
		return nil, err
	}

	senderTransferAmountHi, err := m.ToAmountHi.ToCiphertext()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountLo, err := m.ToAmountHi.ToCiphertext()
	if err != nil {
		return nil, err
	}

	recipientTransferAmountHi, err := m.ToAmountHi.ToCiphertext()
	if err != nil {
		return nil, err
	}

	remainingBalanceCommitment, err := m.ToAmountHi.ToCiphertext()
	if err != nil {
		return nil, err
	}

	//proofs := m.Proofs.To

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
		//Proofs:                     proofs,
	}, nil
}
