package types

import (
	"strings"

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
	if !common.IsHexAddress(msg.Claimer) {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid claimer address (%s)", msg.Claimer)
	}
	for _, asset := range msg.Assets {
		switch asset.AssetType {
		case AssetType_TYPECW20, AssetType_TYPECW721:
			if _, err = sdk.AccAddressFromBech32(asset.ContractAddress); err != nil {
				return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "Invalid contract address (%s)", err)
			}
		case AssetType_TYPENATIVE:
			if strings.TrimSpace(asset.Denom) == "" {
				return sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "Invalid denom %s", asset.Denom)
			}
			if err := sdk.ValidateDenom(asset.Denom); err != nil {
				return sdkerrors.Wrapf(sdkerrors.ErrInvalidCoins, "Invalid denom %s", asset.Denom)
			}
		default:
			return sdkerrors.Wrapf(sdkerrors.ErrUnknownRequest, "Unsupported asset type %s", asset.AssetType)
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
