package types

type OrderBook interface {
	GetId() uint64
	GetEntry() *OrderEntry
}
