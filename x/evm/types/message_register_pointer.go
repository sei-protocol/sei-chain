package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

const TypeMsgRegisterPointer = "evm_register_pointer"

var (
	_ seitypes.Msg = &MsgRegisterPointer{}
)

func NewMsgRegisterERC20Pointer(sender seitypes.AccAddress, ercAddress common.Address) *MsgRegisterPointer {
	return &MsgRegisterPointer{Sender: sender.String(), ErcAddress: ercAddress.Hex(), PointerType: PointerType_ERC20}
}

func NewMsgRegisterERC721Pointer(sender seitypes.AccAddress, ercAddress common.Address) *MsgRegisterPointer {
	return &MsgRegisterPointer{Sender: sender.String(), ErcAddress: ercAddress.Hex(), PointerType: PointerType_ERC721}
}

func NewMsgRegisterERC1155Pointer(sender seitypes.AccAddress, ercAddress common.Address) *MsgRegisterPointer {
	return &MsgRegisterPointer{Sender: sender.String(), ErcAddress: ercAddress.Hex(), PointerType: PointerType_ERC1155}
}

func (msg *MsgRegisterPointer) Route() string {
	return RouterKey
}

func (msg *MsgRegisterPointer) Type() string {
	return TypeMsgRegisterPointer
}

func (msg *MsgRegisterPointer) GetSigners() []seitypes.AccAddress {
	from, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{from}
}

func (msg *MsgRegisterPointer) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

func (msg *MsgRegisterPointer) ValidateBasic() error {
	_, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	if !common.IsHexAddress(msg.ErcAddress) {
		return sdkerrors.ErrInvalidAddress
	}

	return nil
}
