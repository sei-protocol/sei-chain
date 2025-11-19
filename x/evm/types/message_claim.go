package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

const TypeMsgClaim = "evm_claim"

var (
	_ seitypes.Msg = &MsgClaim{}
)

func NewMsgClaim(sender seitypes.AccAddress, claimer common.Address) *MsgClaim {
	return &MsgClaim{Sender: sender.String(), Claimer: claimer.Hex()}
}

func (msg *MsgClaim) Route() string {
	return RouterKey
}

func (msg *MsgClaim) Type() string {
	return TypeMsgClaim
}

func (msg *MsgClaim) GetSigners() []seitypes.AccAddress {
	from, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []seitypes.AccAddress{from}
}

func (msg *MsgClaim) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

func (msg *MsgClaim) ValidateBasic() error {
	_, err := seitypes.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}

	return nil
}
