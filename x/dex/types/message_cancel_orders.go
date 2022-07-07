package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgCancelOrders = "cancel_orders"

var _ sdk.Msg = &MsgCancelOrders{}

func NewMsgCancelOrders(
	creator string,
	orderIds []uint64,
	contractAddr string,
) *MsgCancelOrders {
	return &MsgCancelOrders{
		Creator:      creator,
		OrderIds:     orderIds,
		ContractAddr: contractAddr,
	}
}

func (msg *MsgCancelOrders) Route() string {
	return RouterKey
}

func (msg *MsgCancelOrders) Type() string {
	return TypeMsgCancelOrders
}

func (msg *MsgCancelOrders) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgCancelOrders) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgCancelOrders) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}
	return nil
}
