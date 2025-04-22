package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
)

const TypeMsgClaimSpecific = "evm_claim_specific"

var (
	_ sdk.Msg = &MsgClaimSpecific{}
)

func NewMsgClaimSpecificCW20(sender sdk.AccAddress, claimer common.Address, contract sdk.AccAddress) *MsgClaimSpecific {
	return &MsgClaimSpecific{Sender: sender.String(), Claimer: claimer.Hex(), Identifier: contract.String(), AssetType: AssetType_TYPECW20}
}

func NewMsgClaimSpecificCW721(sender sdk.AccAddress, claimer common.Address, contract sdk.AccAddress) *MsgClaimSpecific {
	return &MsgClaimSpecific{Sender: sender.String(), Claimer: claimer.Hex(), Identifier: contract.String(), AssetType: AssetType_TYPECW721}
}

func (msg *MsgClaimSpecific) Route() string {
	return RouterKey
}

func (msg *MsgClaimSpecific) Type() string {
	return TypeMsgClaimSpecific
}

func (msg *MsgClaimSpecific) GetSigners() []sdk.AccAddress {
	from, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{from}
}

func (msg *MsgClaimSpecific) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}

func (msg *MsgClaimSpecific) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Sender)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid sender address (%s)", err)
	}
	_, err = sdk.AccAddressFromBech32(msg.Identifier)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid contract address (%s)", err)
	}

	return nil
}

func (msg *MsgClaimSpecific) IsCW20() bool {
	return msg.AssetType == AssetType_TYPECW20
}

func (msg *MsgClaimSpecific) IsCW721() bool {
	return msg.AssetType == AssetType_TYPECW721
}
