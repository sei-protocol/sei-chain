package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgRegisterPairs = "register_pairs"

var _ sdk.Msg = &MsgRegisterPairs{}

func NewMsgRegisterPairs(
	creator string,
	contractPairs []BatchContractPair,
) *MsgRegisterPairs {
	return &MsgRegisterPairs{
		Creator:           creator,
		Batchcontractpair: contractPairs,
	}
}

func (msg *MsgRegisterPairs) Route() string {
	return RouterKey
}

func (msg *MsgRegisterPairs) Type() string {
	return TypeMsgRegisterPairs
}

func (msg *MsgRegisterPairs) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgRegisterPairs) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgRegisterPairs) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}
	return nil
}
