package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgCancelOrders = "cancel_orders"

var _ sdk.Msg = &MsgCancelOrders{}

func NewMsgCancelOrders(
	creator string,
	cancellations []*Cancellation,
	contractAddr string,
) *MsgCancelOrders {
	return &MsgCancelOrders{
		Creator:       creator,
		Cancellations: cancellations,
		ContractAddr:  contractAddr,
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

	_, err = sdk.AccAddressFromBech32(msg.ContractAddr)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid contract address (%s)", err)
	}

	for _, cancellation := range msg.Cancellations {
		if cancellation.Price.IsNil() {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid cancellation price (%s)", err)
		}
		if cancellation.Price.IsNegative() {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid cancellation price (cannot be negative) (%s)", err)
		}
		if len(cancellation.AssetDenom) == 0 || sdk.ValidateDenom(cancellation.AssetDenom) != nil {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid cancellation, asset denom is empty or invalid (%s)", err)
		}
		if len(cancellation.PriceDenom) == 0 || sdk.ValidateDenom(cancellation.PriceDenom) != nil {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid cancellation, price denom is empty or invalid (%s)", err)
		}
	}

	return nil
}
