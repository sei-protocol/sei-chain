package types

import "fmt"

type ContractAddress string
type PairString string

func GetPairString(pair *Pair) PairString {
	return PairString(
		fmt.Sprintf("%s|%s",
			GetContractDenomName(pair.PriceDenom),
			GetContractDenomName(pair.AssetDenom),
		),
	)
}
