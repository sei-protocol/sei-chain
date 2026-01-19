package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/utils"
)

const TypeMsgClaimSpecific = "evm_claim_specific"

var (
	_ sdk.Msg = &MsgClaimSpecific{}
)

func NewMsgClaimSpecific(sender sdk.AccAddress, claimer common.Address, assets ...*Asset) *MsgClaimSpecific {
	return &MsgClaimSpecific{Sender: sender.String(), Claimer: claimer.Hex(), Assets: assets}
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
	for _, asset := range msg.Assets {
		_, err = sdk.AccAddressFromBech32(asset.ContractAddress)
		if err != nil {
			return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid contract address (%s)", err)
		}
	}

	return nil
}

func (msg *MsgClaimSpecific) GetIAssets() (res []utils.IAsset) {
	for _, a := range msg.Assets {
		res = append(res, a)
	}
	return
}

func (a *Asset) IsCW20() bool {
	return a.AssetType == AssetType_TYPECW20
}

func (a *Asset) IsCW721() bool {
	return a.AssetType == AssetType_TYPECW721
}

func (a *Asset) IsNative() bool {
	return a.AssetType == AssetType_TYPENATIVE
}
