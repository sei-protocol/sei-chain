package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// constants
const (
	TypeMsgRecordTransactionData = "record_transaction_data"
)

var _ sdk.Msg = &MsgRecordTransactionData{}

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
