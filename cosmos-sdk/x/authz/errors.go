package authz

import (
	sdkerrors "github.com/sei-protocol/sei-chain/cosmos-sdk/types/errors"
)

// x/authz module sentinel errors
var (
	ErrInvalidExpirationTime = sdkerrors.Register(ModuleName, 3, "expiration time of authorization should be more than current time")
)
