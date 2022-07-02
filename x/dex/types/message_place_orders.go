package types

import (
	"errors"
	math "math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgPlaceOrders = "place_orders"

var _ sdk.Msg = &MsgPlaceOrders{}

func NewMsgPlaceOrders(
	creator string,
	orders []*OrderPlacement,
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

func (msg *MsgPlaceOrders) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}
	for _, orderplacement := range msg.Orders {
		// orderplacement.AssetDenom
		val, err := orderplacement.Price.Float64()
		if err != nil {
			return sdkerrors.Wrapf(errors.New("parsePriceErr"), "fail to parse price of the order")
		}
		tickSize := 2
		if val < float64(tickSize) || math.Mod(val, float64(tickSize)) != 0 {
			return sdkerrors.Wrapf(ErrIntOverflowTickSize, "price need to be multiple of tick size")
		}
	}
	return nil
}