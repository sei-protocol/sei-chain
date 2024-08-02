package types

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUpdatePriceTickSize = "update_price_tick_size"

var _ sdk.Msg = &MsgUpdatePriceTickSize{}

func NewMsgUpdatePriceTickSize(
	creator string,
	tickSizeList []TickSize,
) *MsgUpdatePriceTickSize {
	return &MsgUpdatePriceTickSize{
		Creator:      creator,
		TickSizeList: tickSizeList,
	}
}

func (msg *MsgUpdatePriceTickSize) Route() string {
	return RouterKey
}

func (msg *MsgUpdatePriceTickSize) Type() string {
	return TypeMsgUpdatePriceTickSize
}

func (msg *MsgUpdatePriceTickSize) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgUpdatePriceTickSize) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgUpdatePriceTickSize) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	if len(msg.TickSizeList) == 0 {
		return errors.New("no data provided in update price tick size transaction")
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
