package types

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
)

type OrderBook struct {
	Contract ContractAddress
	Pair     Pair
	Longs    *CachedSortedOrderBookEntries
	Shorts   *CachedSortedOrderBookEntries
}

// entries are always sorted by prices in ascending order, regardless of side
type CachedSortedOrderBookEntries struct {
	CachedEntries  []OrderBookEntry
	currentPtr     int
	currentChanged bool

	loader  func(ctx sdk.Context, startingPriceExclusive sdk.Dec, withLimit bool) []OrderBookEntry
	setter  func(sdk.Context, OrderBookEntry)
	deleter func(sdk.Context, OrderBookEntry)
}

func NewCachedSortedOrderBookEntries(
	loader func(ctx sdk.Context, startingPriceExclusive sdk.Dec, withLimit bool) []OrderBookEntry,
	setter func(sdk.Context, OrderBookEntry),
	deleter func(sdk.Context, OrderBookEntry),
) *CachedSortedOrderBookEntries {
	return &CachedSortedOrderBookEntries{
		CachedEntries:  []OrderBookEntry{},
		currentPtr:     0,
		currentChanged: false,
		loader:         loader,
		setter:         setter,
		deleter:        deleter,
	}
}

func (c *CachedSortedOrderBookEntries) load(ctx sdk.Context) {
	var loaded []OrderBookEntry
	if len(c.CachedEntries) == 0 {
		loaded = c.loader(ctx, sdk.ZeroDec(), false)
	} else {
		loaded = c.loader(ctx, c.CachedEntries[len(c.CachedEntries)-1].GetOrderEntry().Price, true)
	}
	c.CachedEntries = append(c.CachedEntries, loaded...)
}

// Reduce quantity of the order book entry currently being pointed at by the specified quantity.
// Also remove/reduce allocations of the order book entry in FIFO order. If the order book entry
// does not have enough quantity to settle against, the returned `settled` value will equal to
// the quantity of the order book entry; otherwise it will equal to the specified quantity.
func (c *CachedSortedOrderBookEntries) SettleQuantity(_ sdk.Context, quantity sdk.Dec) (res []ToSettle, settled sdk.Dec) {
	if quantity.IsZero() {
		return []ToSettle{}, quantity
	}
	currentEntry := c.CachedEntries[c.currentPtr].GetOrderEntry()
	c.currentChanged = true

	if quantity.GTE(currentEntry.Quantity) {
		res = utils.Map(currentEntry.Allocations, AllocationToSettle)
		settled = currentEntry.Quantity
		currentEntry.Quantity = sdk.ZeroDec()
		currentEntry.Allocations = []*Allocation{}
		return res, settled
	}

	settled = sdk.ZeroDec()
	newFirstAllocationIdx := 0
	for idx, a := range currentEntry.Allocations {
		postSettle := settled.Add(a.Quantity)
		if postSettle.LTE(quantity) {
			settled = postSettle
			res = append(res, AllocationToSettle(a))
		} else {
			newFirstAllocationIdx = idx
			if settled.Equal(quantity) {
				break
			}
			res = append(res, ToSettle{
				OrderID: a.OrderId,
				Account: a.Account,
				Amount:  quantity.Sub(settled),
			})
			a.Quantity = a.Quantity.Sub(quantity.Sub(settled))
			settled = quantity
			break
		}
	}
	currentEntry.Quantity = currentEntry.Quantity.Sub(quantity)
	currentEntry.Allocations = currentEntry.Allocations[newFirstAllocationIdx:]
	return res, settled
}

// Discard all dirty changes and reload
func (c *CachedSortedOrderBookEntries) Refresh(ctx sdk.Context) {
	c.CachedEntries = c.loader(ctx, sdk.ZeroDec(), false)
	c.currentPtr = 0
	c.currentChanged = false
}

func (c *CachedSortedOrderBookEntries) Flush(ctx sdk.Context) {
	stop := c.currentPtr
	if !c.currentChanged {
		stop--
	}
	for i := 0; i <= stop; i++ {
		if i >= len(c.CachedEntries) {
			break
		}
		entry := c.CachedEntries[i]
		if entry.GetOrderEntry().Quantity.IsZero() {
			c.deleter(ctx, entry)
		} else {
			c.setter(ctx, entry)
		}
	}
	c.CachedEntries = c.CachedEntries[c.currentPtr:]
	c.currentPtr = 0
	c.currentChanged = false
}

// Next will only move on to the next order if the current order quantity hits zero.
// So it should not be used for read-only iteration
func (c *CachedSortedOrderBookEntries) Next(ctx sdk.Context) OrderBookEntry {
	for c.currentPtr < len(c.CachedEntries) && c.CachedEntries[c.currentPtr].GetOrderEntry().Quantity.IsZero() {
		c.currentPtr++
		c.currentChanged = false
	}
	if c.currentPtr >= len(c.CachedEntries) {
		c.load(ctx)
		// if nothing is loaded, we've reached the end
		if c.currentPtr >= len(c.CachedEntries) {
			return nil
		}
	}
	return c.CachedEntries[c.currentPtr]
}

type OrderBookEntry interface {
	GetPrice() sdk.Dec
	GetOrderEntry() *OrderEntry
	DeepCopy() OrderBookEntry
	SetEntry(*OrderEntry)
	SetPrice(sdk.Dec)
}

func (m *LongBook) SetPrice(p sdk.Dec) {
	m.Price = p
}

type PriceStore struct {
	Store     prefix.Store
	PriceKeys [][]byte
}

func (m *LongBook) GetPrice() sdk.Dec {
	return m.Price
}

func (m *LongBook) GetOrderEntry() *OrderEntry {
	return m.Entry
}

func (m *LongBook) SetEntry(newEntry *OrderEntry) {
	m.Entry = newEntry
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

func (m *ShortBook) SetPrice(p sdk.Dec) {
	m.Price = p
}

func (m *ShortBook) GetPrice() sdk.Dec {
	return m.Price
}

func (m *ShortBook) GetOrderEntry() *OrderEntry {
	return m.Entry
}

func (m *ShortBook) SetEntry(newEntry *OrderEntry) {
	m.Entry = newEntry
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

type ToSettle struct {
	OrderID uint64
	Amount  sdk.Dec
	Account string
}

func AllocationToSettle(a *Allocation) ToSettle {
	return ToSettle{
		OrderID: a.OrderId,
		Amount:  a.Quantity,
		Account: a.Account,
	}
}
