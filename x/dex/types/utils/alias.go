package utils

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type (
	ContractAddress string
	PairString      string
)

func GetPairString(pair *types.Pair) PairString {
	return PairString(
		fmt.Sprintf("%s|%s", pair.PriceDenom, pair.AssetDenom),
	)
}
