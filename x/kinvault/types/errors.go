package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

var ErrNotImplemented = sdkerrors.Register(ModuleName, 1, "not implemented")
