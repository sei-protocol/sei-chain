package utils

import "github.com/sei-protocol/sei-chain/x/dex/types"

var OppositePositionDirection = map[types.PositionDirection]types.PositionDirection{
	types.PositionDirection_LONG:  types.PositionDirection_SHORT,
	types.PositionDirection_SHORT: types.PositionDirection_LONG,
}
