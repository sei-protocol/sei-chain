package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// ErrIBCDeprecated indicates that the IBC module is deprecated.
var ErrIBCDeprecated = sdkerrors.Register("ibc", 103, "ibc module is deprecated")
