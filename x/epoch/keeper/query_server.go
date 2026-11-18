package keeper

import (
	"github.com/sei-protocol/sei-chain/x/epoch/types"
)

var _ types.QueryServer = Keeper{}
