package types

import (
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
)

// ErrVestingDeprecated is returned by every vesting message handler now that
// the module is deprecated. Existing vesting accounts remain in state and
// continue to vest according to their schedules; only the creation of new
// vesting accounts is disabled.
var ErrVestingDeprecated = sdkerrors.Register(ModuleName, 2, "vesting module is deprecated; creating new vesting accounts is disabled")
