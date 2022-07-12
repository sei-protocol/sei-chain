package types

import "fmt"

type (
	ContractAddress string
	PairString      string
)

func GetPairString(pair *Pair) PairString {
	return PairString(
		fmt.Sprintf("%s|%s", pair.PriceDenom, pair.AssetDenom),
	)
}
