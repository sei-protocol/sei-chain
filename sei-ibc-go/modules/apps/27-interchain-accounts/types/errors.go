package types

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	ErrUnknownDataType             = sdkerrors.Register(ModuleName, 2, "unknown data type")
	ErrAccountAlreadyExist         = sdkerrors.Register(ModuleName, 3, "account already exist")
	ErrPortAlreadyBound            = sdkerrors.Register(ModuleName, 4, "port is already bound")
	ErrInvalidChannelFlow          = sdkerrors.Register(ModuleName, 5, "invalid message sent to channel end")
	ErrInvalidOutgoingData         = sdkerrors.Register(ModuleName, 6, "invalid outgoing data")
	ErrInvalidRoute                = sdkerrors.Register(ModuleName, 7, "invalid route")
	ErrInterchainAccountNotFound   = sdkerrors.Register(ModuleName, 8, "interchain account not found")
	ErrInterchainAccountAlreadySet = sdkerrors.Register(ModuleName, 9, "interchain account is already set")
	ErrActiveChannelNotFound       = sdkerrors.Register(ModuleName, 10, "no active channel for this owner")
	ErrInvalidVersion              = sdkerrors.Register(ModuleName, 11, "invalid interchain accounts version")
	ErrInvalidAccountAddress       = sdkerrors.Register(ModuleName, 12, "invalid account address")
	ErrUnsupported                 = sdkerrors.Register(ModuleName, 13, "interchain account does not support this action")
)
