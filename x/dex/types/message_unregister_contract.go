package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUnregisterContract = "unregister_contract"

var _ sdk.Msg = &MsgUnregisterContract{}

func NewMsgUnregisterContract(
	creator string,
	contractAddr string,
) *MsgUnregisterContract {
	return &MsgUnregisterContract{
		Creator:      creator,
		ContractAddr: contractAddr,
	}
}

func (msg *MsgUnregisterContract) Route() string {
	return RouterKey
}

func (msg *MsgUnregisterContract) Type() string {
	return TypeMsgUnregisterContract
}

func (msg *MsgUnregisterContract) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgUnregisterContract) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgUnregisterContract) ValidateBasic() error {
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
