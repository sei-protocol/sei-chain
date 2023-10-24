package ss

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// StoreCodespace defines the store package's unique error code space.
const StoreCodespace = "store"

var (
	ErrKeyEmpty      = sdkerrors.Register(StoreCodespace, 1, "key empty")
	ErrStartAfterEnd = sdkerrors.Register(StoreCodespace, 2, "start key after end key")
)
