package query

import (
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

var _ types.QueryServer = KeeperWrapper{}
