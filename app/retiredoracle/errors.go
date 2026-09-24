package retiredoracle

import "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"

var ErrDeprecated = errors.Register("oracle", 25, "oracle module is deprecated")
