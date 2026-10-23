package precompiles

import (
	"bytes"
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
)

// StaticRegistry is a Registry over a fixed set of contracts.
type StaticRegistry struct {
	contracts   map[common.Address]Contract
	endBlockers []common.Address
}

// NewStaticRegistry returns a Registry serving contracts, which it copies.
// endBlockers lists the addresses whose contracts run EndBlock, in run order.
func NewStaticRegistry(contracts map[common.Address]Contract, endBlockers ...common.Address) StaticRegistry {
	return StaticRegistry{contracts: maps.Clone(contracts), endBlockers: slices.Clone(endBlockers)}
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

func (r StaticRegistry) EndBlockers() []common.Address {
	return slices.Clone(r.endBlockers)
}
