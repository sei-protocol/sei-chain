package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgContractDepositRent = "contract_deposit_rent"

var _ sdk.Msg = &MsgContractDepositRent{}

func NewMsgContractDepositRent(
	contractAddr string,
	amount uint64,
	sender string,
) *MsgContractDepositRent {
	return &MsgContractDepositRent{
		Sender:       sender,
		ContractAddr: contractAddr,
		Amount:       amount,
	}
}

func (msg *MsgContractDepositRent) Route() string {
	return RouterKey
}

func (msg *MsgContractDepositRent) Type() string {
	return TypeMsgContractDepositRent
}

func (msg *MsgContractDepositRent) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgContractDepositRent) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgContractDepositRent) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	_, err = sdk.AccAddressFromBech32(msg.ContractAddr)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid contract address (%s)", err)
	}
	return nil
}
