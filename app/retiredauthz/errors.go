package retiredauthz

import "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"

// ErrDeprecated is the error every retired authz message fails validation with.
var ErrDeprecated = errors.Register(ModuleName, 2, "authz module is deprecated") // code 3 keeps its historical meaning
