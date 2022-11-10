package types

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgUpdateTickSize = "update_tick_size"

var _ sdk.Msg = &MsgUpdateTickSize{}

func NewMsgUpdateTickSize(
	creator string,
	tickSizeList []TickSize,
) *MsgUpdateTickSize {
	return &MsgUpdateTickSize{
		Creator:      creator,
		TickSizeList: tickSizeList,
	}
}

func (msg *MsgUpdateTickSize) Route() string {
	return RouterKey
}

func (msg *MsgUpdateTickSize) Type() string {
	return TypeMsgUpdateTickSize
}

func (msg *MsgUpdateTickSize) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgUpdateTickSize) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgUpdateTickSize) ValidateBasic() error {
	if msg.Creator == "" {
		return errors.New("creator address is empty")
	}

	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	if len(msg.TickSizeList) == 0 {
		return errors.New("no data provided in update tick size transaction")
	}

	for _, tickSize := range msg.TickSizeList {
		contractAddress := tickSize.ContractAddr

		if contractAddress == "" {
			return errors.New("contract address is empty")
		}

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
