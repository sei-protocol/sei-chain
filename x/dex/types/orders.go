package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type OrderBook struct {
	Longs  *CachedSortedOrderBookEntries
	Shorts *CachedSortedOrderBookEntries
}

// entries are always sorted by prices in ascending order, regardless of side
type CachedSortedOrderBookEntries struct {
	Entries      []OrderBookEntry
	DirtyEntries map[string]OrderBookEntry
}

func (c *CachedSortedOrderBookEntries) AddDirtyEntry(entry OrderBookEntry) {
	c.DirtyEntries[entry.GetPrice().String()] = entry
}

type OrderBookEntry interface {
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
