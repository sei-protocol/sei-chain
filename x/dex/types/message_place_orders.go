package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgPlaceOrders = "place_orders"

var _ sdk.Msg = &MsgPlaceOrders{}

func NewMsgPlaceOrders(
	creator string,
	orders []*Order,
	contractAddr string,
	fund sdk.Coins,
) *MsgPlaceOrders {
	return &MsgPlaceOrders{
		Creator:      creator,
		Orders:       orders,
		ContractAddr: contractAddr,
		Funds:        fund,
	}
}

func (msg *MsgPlaceOrders) Route() string {
	return RouterKey
}

func (msg *MsgPlaceOrders) Type() string {
	return TypeMsgPlaceOrders
}

func (msg *MsgPlaceOrders) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgPlaceOrders) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

// perform statelss check on basic property of msg like sig verification
func (msg *MsgPlaceOrders) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(msg.ContractAddr)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid contract address (%s)", err)
	}

	if len(msg.Orders) == 0 {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "at least one order needs to be placed (%s)", err)
	}

	for _, order := range msg.Orders {
		if order.Quantity.IsNil() || order.Quantity.IsNegative() {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid order quantity (%s)", err)
		}
		if order.Price.IsNil() || order.Price.IsNegative() {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid order price (%s)", err)
		}
		if len(order.AssetDenom) == 0 || sdk.ValidateDenom(order.AssetDenom) != nil {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid order, asset denom is empty or invalid (%s)", err)
		}
		if len(order.PriceDenom) == 0 || sdk.ValidateDenom(order.PriceDenom) != nil {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid order, price denom is empty or invalid (%s)", err)
		}
		if order.OrderType == OrderType_FOKMARKETBYVALUE || order.OrderType == OrderType_FOKMARKET {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "FOK orders are temporarily disabled")
		}
		if order.OrderType == OrderType_STOPLIMIT || order.OrderType == OrderType_STOPLOSS {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidRequest, "stop loss/limit order (%s) are not supported yet", err)
		}
	}

	return nil
}
