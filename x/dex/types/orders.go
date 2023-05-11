package types

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
)

type OrderBook struct {
	Longs  *CachedSortedOrderBookEntries
	Shorts *CachedSortedOrderBookEntries
}

func (o *OrderBook) DeepCopy() *OrderBook {
	return &OrderBook{
		Longs:  o.Longs.DeepCopy(),
		Shorts: o.Shorts.DeepCopy(),
	}
}

// entries are always sorted by prices in ascending order, regardless of side
type CachedSortedOrderBookEntries struct {
	Entries      []OrderBookEntry
	DirtyEntries *datastructures.TypedSyncMap[string, OrderBookEntry]
}

func (c *CachedSortedOrderBookEntries) DeepCopy() *CachedSortedOrderBookEntries {
	return &CachedSortedOrderBookEntries{
		Entries:      utils.Map(c.Entries, func(e OrderBookEntry) OrderBookEntry { return e.DeepCopy() }),
		DirtyEntries: c.DirtyEntries.DeepCopy(func(e OrderBookEntry) OrderBookEntry { return e.DeepCopy() }),
	}
}

func (c *CachedSortedOrderBookEntries) AddDirtyEntry(entry OrderBookEntry) {
	c.DirtyEntries.Store(entry.GetPrice().String(), entry)
}

type OrderBookEntry interface {
	GetPrice() sdk.Dec
	GetEntry() *OrderEntry
	DeepCopy() OrderBookEntry
}

type PriceStore struct {
	Store     prefix.Store
	PriceKeys [][]byte
}

func (m *LongBook) GetPrice() sdk.Dec {
	if m != nil {
		return m.Price
	}
	return sdk.ZeroDec()
}

func (m *LongBook) DeepCopy() OrderBookEntry {
	allocations := []*Allocation{}
	for _, allo := range m.Entry.Allocations {
		allocations = append(allocations, &Allocation{
			OrderId:  allo.OrderId,
			Quantity: allo.Quantity,
			Account:  allo.Account,
		})
	}
	newOrderEntry := OrderEntry{
		Price:       m.Entry.Price,
		Quantity:    m.Entry.Quantity,
		PriceDenom:  m.Entry.PriceDenom,
		AssetDenom:  m.Entry.AssetDenom,
		Allocations: allocations,
	}
	return &LongBook{
		Price: m.Price,
		Entry: &newOrderEntry,
	}
}

func (m *ShortBook) GetPrice() sdk.Dec {
	if m != nil {
		return m.Price
	}
	return sdk.ZeroDec()
}

func (m *ShortBook) DeepCopy() OrderBookEntry {
	allocations := []*Allocation{}
	for _, allo := range m.Entry.Allocations {
		allocations = append(allocations, &Allocation{
			OrderId:  allo.OrderId,
			Quantity: allo.Quantity,
			Account:  allo.Account,
		})
	}
	newOrderEntry := OrderEntry{
		Price:       m.Entry.Price,
		Quantity:    m.Entry.Quantity,
		PriceDenom:  m.Entry.PriceDenom,
		AssetDenom:  m.Entry.AssetDenom,
		Allocations: allocations,
	}
	return &ShortBook{
		Price: m.Price,
		Entry: &newOrderEntry,
	}
}
