package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

const TypeMsgAssociateContractAddress = "evm_associate_contract_address"

var (
	_ seitypes.Msg = &MsgAssociateContractAddress{}
)

func NewMsgAssociateContractAddress(sender seitypes.AccAddress, addr seitypes.AccAddress) *MsgAssociateContractAddress {
	return &MsgAssociateContractAddress{Sender: sender.String(), Address: addr.String()}
}

func (msg *MsgAssociateContractAddress) Route() string {
	return RouterKey
}

func (msg *MsgAssociateContractAddress) Type() string {
	return TypeMsgAssociateContractAddress
}

func (msg *MsgAssociateContractAddress) GetSigners() []seitypes.AccAddress {
	from, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{from}
}

func (msg *MsgAssociateContractAddress) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

func (msg *MsgAssociateContractAddress) ValidateBasic() error {
	_, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	if _, err := seitypes.AccAddressFromBech32(msg.Address); err != nil {
		return sdkerrors.ErrInvalidAddress
	}

	return nil
}
