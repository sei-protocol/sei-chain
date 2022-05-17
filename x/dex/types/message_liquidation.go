package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgLiquidation = "liquidation"

var _ sdk.Msg = &MsgLiquidation{}

func NewMsgLiquidation(
	creator string,
	contractAddr string,
	accountToLiquidate string,
	nonce uint64,
) *MsgLiquidation {
	return &MsgLiquidation{
		Creator:            creator,
		ContractAddr:       contractAddr,
		AccountToLiquidate: accountToLiquidate,
		Nonce:              nonce,
	}
}

func (msg *MsgLiquidation) Route() string {
	return RouterKey
}

func (msg *MsgLiquidation) Type() string {
	return TypeMsgLiquidation
}

func (msg *MsgLiquidation) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgLiquidation) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgLiquidation) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}
	return nil
}
