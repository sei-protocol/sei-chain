package retiredvesting

import "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"

// ErrDeprecated is the error every retired vesting message fails validation with.
var ErrDeprecated = errors.Register(ModuleName, 2, "vesting module is deprecated; creating new vesting accounts is disabled")
