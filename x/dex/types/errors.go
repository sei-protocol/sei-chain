package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// x/dex module sentinel errors
var (
	ErrSample                 = sdkerrors.Register(ModuleName, 1100, "sample error")
	InsufficientAssetError    = sdkerrors.Register(ModuleName, 1101, "insufficient fund")
	UnknownCustomMessageError = sdkerrors.Register(ModuleName, 1102, "unknown custom message")
)
