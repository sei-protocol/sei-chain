package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// MaxMemoCharLength defines the maximum length for the InterchainAccountPacketData memo field
const MaxMemoCharLength = 256

// ValidateBasic performs basic validation of the interchain account packet data.
// The memo may be empty.
func (iapd InterchainAccountPacketData) ValidateBasic() error {
	if iapd.Type == UNSPECIFIED {
		return sdkerrors.Wrap(ErrInvalidOutgoingData, "packet data type cannot be unspecified")
	}

	if iapd.Data == nil {
		return sdkerrors.Wrap(ErrInvalidOutgoingData, "packet data cannot be empty")
	}

	if len(iapd.Memo) > MaxMemoCharLength {
		return sdkerrors.Wrapf(ErrInvalidOutgoingData, "packet data memo cannot be greater than %d characters", MaxMemoCharLength)
	}

	return nil
}

// GetBytes returns the JSON marshalled interchain account packet data.
func (iapd InterchainAccountPacketData) GetBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&iapd))
}

// GetBytes returns the JSON marshalled interchain account CosmosTx.
func (ct CosmosTx) GetBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(&ct))
}

// UnpackInterfaces implements UnpackInterfacesMessage.UnpackInterfaces
func (ct CosmosTx) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	for _, any := range ct.Messages {
		err := unpacker.UnpackAny(any, new(sdk.Msg))
		if err != nil {
			return err
		}
	}

	return nil
}
