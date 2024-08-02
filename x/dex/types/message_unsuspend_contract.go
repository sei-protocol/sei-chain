package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUnsuspendContract = "unsuspend_contract"

var _ sdk.Msg = &MsgUnsuspendContract{}

func NewMsgUnsuspendContract(
	creator string,
	contractAddr string,
) *MsgUnsuspendContract {
	return &MsgUnsuspendContract{
		Creator:      creator,
		ContractAddr: contractAddr,
	}
}

func (msg *MsgUnsuspendContract) Route() string {
	return RouterKey
}

func (msg *MsgUnsuspendContract) Type() string {
	return TypeMsgUnsuspendContract
}

func (msg *MsgUnsuspendContract) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgUnsuspendContract) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgUnsuspendContract) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(msg.ContractAddr)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid contract address (%s)", err)
	}

	return nil
}
