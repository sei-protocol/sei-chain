package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// x/epoch module sentinel errors
var (
	ErrParsingSeiEpochQuery = sdkerrors.Register(ModuleName, 2, "Error parsing SeiEpochQuery")
	ErrGettingEpoch         = sdkerrors.Register(ModuleName, 3, "Error while getting epoch")
	ErrEncodingEpoch        = sdkerrors.Register(ModuleName, 4, "Error encoding epoch as JSON")
	ErrUnknownSeiEpochQuery = sdkerrors.Register(ModuleName, 6, "Error unknown sei epoch query")
)
