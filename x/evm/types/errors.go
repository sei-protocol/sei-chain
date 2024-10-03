package types

import (
	"fmt"
	"strings"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type AssociationMissingErr struct {
	Address string
}

func NewAssociationMissingErr(address string) AssociationMissingErr {
	return AssociationMissingErr{Address: address}
}

func (e AssociationMissingErr) Error() string {
	return fmt.Sprintf("address %s is not linked", e.Address)
}

func (e AssociationMissingErr) AddressType() string {
	if strings.HasPrefix(e.Address, "0x") {
		return "evm"
	}
	return "sei"
}

const EvmCodespace = "evm"

var (
	errInternal = sdkerrors.Register(EvmCodespace, 1, "internal")

	ErrInvalidBech32 = sdkerrors.Register(EvmCodespace, 2, "invalid bech32 address representation")

	ErrMoreThanOneHop = sdkerrors.Register(EvmCodespace, 3, "sei does not support EVM->CW->EVM call pattern")

	ErrMaxInitCodeSize = sdkerrors.Register(EvmCodespace, 4, "max init code size exceeded")

	ErrEVMExecution = sdkerrors.Register(EvmCodespace, 5, "execution error")

	ErrResult = sdkerrors.Register(EvmCodespace, 6, "error in execution result")

	ErrFinalizeState = sdkerrors.Register(EvmCodespace, 7, "error in stateDB finalization")

	ErrWriteReceipt = sdkerrors.Register(EvmCodespace, 8, "error writing receipt")
)
