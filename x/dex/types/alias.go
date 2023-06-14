package types

import (
	"fmt"
	"strings"
)

type (
	ContractAddress string
	PairString      string
)

const PairDelim = "|"

func GetPairString(pair *Pair) PairString {
	return PairString(
		fmt.Sprintf("%s%s%s", pair.PriceDenom, PairDelim, pair.AssetDenom),
	)
}

func GetPriceAssetString(pairString PairString) (string, string) {
	output := strings.Split(string(pairString), "|")
	return output[0], output[1]
}
