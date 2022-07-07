package exchange

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type DirtyPrices struct {
	prices []sdk.Dec
}

func NewDirtyPrices() DirtyPrices {
	return DirtyPrices{
		prices: []sdk.Dec{},
	}
}

func (d *DirtyPrices) Add(priceToAdd sdk.Dec) {
	if !d.Has(priceToAdd) {
		d.prices = append(d.prices, priceToAdd)
	}
}

func (d *DirtyPrices) Has(priceToCheck sdk.Dec) bool {
	for _, price := range d.prices {
		if price.Equal(priceToCheck) {
			return true
		}
	}
	return false
}

func (d *DirtyPrices) Get() []sdk.Dec {
	return d.prices
}
