package types

import (
	"fmt"
	"strings"
)

type (
	ContractAddress string
	PairString      string
)

const Separator = "|"

func GetPairString(pair *Pair) PairString {
	return PairString(
		fmt.Sprintf("%s%s%s", pair.PriceDenom, Separator, pair.AssetDenom),
	)
}

func GetPair(s PairString) *Pair {
	parts := strings.Split(string(s), Separator)
	return &Pair{
		PriceDenom: parts[0],
		AssetDenom: parts[1],
	}
}

func GetPriceAssetString(pairString PairString) (string, string) {
	output := strings.Split(string(pairString), "|")
	return output[0], output[1]
}
