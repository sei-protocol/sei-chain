package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgRegisterPair = "register_pair"

var _ sdk.Msg = &MsgRegisterPair{}

func NewMsgRegisterPair(
	creator string,
	contractAddr string,
	priceDenom string,
	assetDenom string,
) *MsgRegisterPair {
	return &MsgRegisterPair{
		Creator:      creator,
		ContractAddr: contractAddr,
		Pair: &Pair{
			PriceDenom: priceDenom,
			AssetDenom: assetDenom,
		},
	}
}

func (msg *MsgRegisterPair) Route() string {
	return RouterKey
}

func (msg *MsgRegisterPair) Type() string {
	return TypeMsgRegisterPair
}

func (msg *MsgRegisterPair) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgRegisterPair) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgRegisterPair) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}
	return nil
}
