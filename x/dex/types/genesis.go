package types

import (
	"fmt"
)

// DefaultIndex is the default capability global index
const DefaultIndex uint64 = 1

// DefaultGenesis returns the default Capability genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:        DefaultParams(),
		ContractState: []ContractState{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	paramErr := gs.Params.Validate()
	if paramErr != nil {
		return paramErr
	}
	for _, cs := range gs.ContractState {
		csErr := cs.Validate()
		if csErr != nil {
			return csErr
		}
	}
	return nil
}

func (cs ContractState) Validate() error {
	if len(cs.ContractInfo.ContractAddr) == 0 {
		return fmt.Errorf("empty contract addr")
	}
	// Check for duplicated ID in shortBook
	longBookIDMap := make(map[uint64]bool)
	for _, elem := range cs.LongBookList {
		if _, ok := longBookIDMap[elem.Price.BigInt().Uint64()]; ok {
			return fmt.Errorf("duplicated price for longBook")
		}
		longBookIDMap[elem.Price.BigInt().Uint64()] = true
	}
	// Check for duplicated ID in shortBook
	shortBookIDMap := make(map[uint64]bool)
	for _, elem := range cs.ShortBookList {
		if _, ok := shortBookIDMap[elem.Price.BigInt().Uint64()]; ok {
			return fmt.Errorf("duplicated price for shortBook")
		}
		shortBookIDMap[elem.Price.BigInt().Uint64()] = true
	}
	return nil
}
