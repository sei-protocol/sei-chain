package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// constants
const (
	TypeMsgRecordTransactionData = "record_transaction_data"
	TypeMsgSubmitFraudChallenge  = "submit_frud_challenge"
)

var (
	_ sdk.Msg = &MsgRecordTransactionData{}
	_ sdk.Msg = &MsgSubmitFraudChallenge{}
)

func NewMsgRecordTransactionData(sender string, slot uint64, root string, txs []string) *MsgRecordTransactionData {
	return &MsgRecordTransactionData{
		Sender:    sender,
		Slot:      slot,
		StateRoot: root,
		Txs:       txs,
	}
}

func (m MsgRecordTransactionData) Route() string { return RouterKey }
func (m MsgRecordTransactionData) Type() string  { return TypeMsgRecordTransactionData }
func (m MsgRecordTransactionData) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	return nil
}

func (m MsgRecordTransactionData) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgRecordTransactionData) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.Sender)
	return []sdk.AccAddress{sender}
}

func NewMsgSubmitFraudChallenge(
	sender string,
	startSlot uint64,
	endSlot uint64,
	fraudStatePubkey string,
	merkleProof *MerkleProof,
	accountStates []*Account,
	programs []*Account,
	sysvarAccounts []*Account,
) *MsgSubmitFraudChallenge {
	return &MsgSubmitFraudChallenge{
		Sender:           sender,
		StartSlot:        startSlot,
		EndSlot:          endSlot,
		FraudStatePubKey: fraudStatePubkey,
		MerkleProof:      merkleProof,
		AccountStates:    accountStates,
		Programs:         programs,
		SysvarAccounts:   sysvarAccounts,
	}
}

func (m MsgSubmitFraudChallenge) Route() string { return RouterKey }
func (m MsgSubmitFraudChallenge) Type() string  { return TypeMsgSubmitFraudChallenge }
func (m MsgSubmitFraudChallenge) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	// check if merkle proof hash has proper length
	if len(m.MerkleProof.Hash) > int(DefaultMaxHashLength) {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "Invalid hash length (%s)", err)
	}

	// check if challenge period is too long
	if m.EndSlot - m.StartSlot > DefaultMaxChallengePeriod {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "Invalid challenge period (%s)", err)
	}

	// check if merkle proof hash size is too large
	for _, hash := range m.MerkleProof.Hash {
        if len(hash) >= int(DefaultMaxHashSize) {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "Invalid challenge period (%s)", err)
        }
    }

	return nil
}

func (m MsgSubmitFraudChallenge) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&m))
}

func (m MsgSubmitFraudChallenge) GetSigners() []sdk.AccAddress {
	sender, _ := sdk.AccAddressFromBech32(m.Sender)
	return []sdk.AccAddress{sender}
}
