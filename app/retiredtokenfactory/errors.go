package retiredtokenfactory

import "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"

// ErrDeprecated is the error every retired tokenfactory message fails validation with.
var ErrDeprecated = errors.Register(ModuleName, 26, "tokenfactory module is deprecated") // codes 2-25 keep their historical meanings
