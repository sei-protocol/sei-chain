package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type OrderBook interface {
	GetPrice() sdk.Dec
	GetEntry() *OrderEntry
}

func (m *LongBook) GetPrice() sdk.Dec {
	if m != nil {
		return m.Price
	}
	return sdk.ZeroDec()
}

func (m *ShortBook) GetPrice() sdk.Dec {
	if m != nil {
		return m.Price
	}
	return sdk.ZeroDec()
}
