package types

import (
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
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
	return nil
}

// Used for concurrent message processing
func (msg *MsgPlaceOrders) GetMsgResourceIdentifier(accessOp sdkacltypes.AccessOperation) string {
	// TODO:: check accessOp for types and return other identifiers
	return fmt.Sprintf(accessOp.GetIdentifierTemplate(), msg.ContractAddr, msg.Creator, msg.Orders)
}
