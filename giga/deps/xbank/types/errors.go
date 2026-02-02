package types

import (
	bankerrors "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// x/bank module sentinel errors
var (
	ErrNoInputs              = bankerrors.ErrNoInputs
	ErrNoOutputs             = bankerrors.ErrNoOutputs
	ErrInputOutputMismatch   = bankerrors.ErrInputOutputMismatch
	ErrSendDisabled          = bankerrors.ErrSendDisabled
	ErrDenomMetadataNotFound = bankerrors.ErrDenomMetadataNotFound
	ErrInvalidKey            = bankerrors.ErrInvalidKey
)
