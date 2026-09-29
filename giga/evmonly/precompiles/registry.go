package precompiles

import (
	"bytes"
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
)

// StaticRegistry is a Registry over a fixed set of contracts.
type StaticRegistry struct {
	contracts map[common.Address]Contract
}

// NewStaticRegistry returns a Registry serving contracts, which it copies.
func NewStaticRegistry(contracts map[common.Address]Contract) StaticRegistry {
	return StaticRegistry{contracts: maps.Clone(contracts)}
}

func (r StaticRegistry) Get(addr common.Address) (Contract, bool) {
	contract, ok := r.contracts[addr]
	return contract, ok
}

// Addresses returns the registered addresses in ascending order.
func (r StaticRegistry) Addresses() []common.Address {
	return slices.SortedFunc(maps.Keys(r.contracts), func(a, b common.Address) int {
		return bytes.Compare(a[:], b[:])
	})
}
