package utils

import "github.com/sei-protocol/sei-chain/x/dex/types"

const (
	OpenOrderCreatorSuffix  = "o"
	CloseOrderCreatorSuffix = "c"
)

var SuffixToPositionEffect = map[string]types.PositionEffect{
	OpenOrderCreatorSuffix:  types.PositionEffect_OPEN,
	CloseOrderCreatorSuffix: types.PositionEffect_CLOSE,
}

var PositionEffectToSuffix = map[types.PositionEffect]string{
	types.PositionEffect_OPEN:  OpenOrderCreatorSuffix,
	types.PositionEffect_CLOSE: CloseOrderCreatorSuffix,
}

const FormattedAccountDelimiter = "|"
