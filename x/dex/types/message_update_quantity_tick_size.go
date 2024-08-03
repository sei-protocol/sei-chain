package types

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUpdateQuantityTickSize = "update_quantity_tick_size"

var _ sdk.Msg = &MsgUpdateQuantityTickSize{}

func NewMsgUpdateQuantityTickSize(
	creator string,
	tickSizeList []TickSize,
) *MsgUpdateQuantityTickSize {
	return &MsgUpdateQuantityTickSize{
		Creator:      creator,
		TickSizeList: tickSizeList,
	}
}

func (msg *MsgUpdateQuantityTickSize) Route() string {
	return RouterKey
}

func (msg *MsgUpdateQuantityTickSize) Type() string {
	return TypeMsgUpdateQuantityTickSize
}

func (msg *MsgUpdateQuantityTickSize) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgUpdateQuantityTickSize) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgUpdateQuantityTickSize) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	if len(msg.TickSizeList) == 0 {
		return errors.New("no data provided in update quantity tick size transaction")
	}

	for _, tickSize := range msg.TickSizeList {
		contractAddress := tickSize.ContractAddr

		_, err = sdk.AccAddressFromBech32(contractAddress)
		if err != nil {
			return errors.New("contract address format is not bech32")
		}

		if tickSize.Pair == nil {
			return errors.New("empty pair info")
		}
	}

	return nil
}
