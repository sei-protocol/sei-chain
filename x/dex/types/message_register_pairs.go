package types

import (
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const TypeMsgRegisterPairs = "register_pairs"

var _ sdk.Msg = &MsgRegisterPairs{}

func NewMsgRegisterPairs(
	creator string,
	contractPairs []BatchContractPair,
) *MsgRegisterPairs {
	return &MsgRegisterPairs{
		Creator:           creator,
		Batchcontractpair: contractPairs,
	}
}

func (msg *MsgRegisterPairs) Route() string {
	return RouterKey
}

func (msg *MsgRegisterPairs) Type() string {
	return TypeMsgRegisterPairs
}

func (msg *MsgRegisterPairs) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{creator}
}

func (msg *MsgRegisterPairs) GetSignBytes() []byte {
	bz := ModuleCdc.MustMarshalJSON(msg)
	return sdk.MustSortJSON(bz)
}

func (msg *MsgRegisterPairs) ValidateBasic() error {
	if msg.Creator == "" {
		return errors.New("creator address is empty")
	}

	_, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return sdkerrors.Wrapf(sdkerrors.ErrInvalidAddress, "invalid creator address (%s)", err)
	}

	if len(msg.Batchcontractpair) == 0 {
		return errors.New("no data provided in register pairs transaction")
	}

	for _, batchContractPair := range msg.Batchcontractpair {
		contractAddress := batchContractPair.ContractAddr

		if contractAddress == "" {
			return errors.New("contract address is empty")
		}

		_, err = sdk.AccAddressFromBech32(contractAddress)
		if err != nil {
			return errors.New("contract address format is not bech32")
		}

		if len(batchContractPair.Pairs) == 0 {
			return errors.New("no pairs provided in register pairs transaction")
		}

		for _, pair := range batchContractPair.Pairs {
			if pair == nil {
				return errors.New("empty pair info")
			}
		}
	}

	return nil
}
