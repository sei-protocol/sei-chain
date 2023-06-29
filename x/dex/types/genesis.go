package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
	if _, err := sdk.AccAddressFromBech32(cs.ContractInfo.ContractAddr); err != nil {
		return fmt.Errorf("contract address is invalid")
	}
	// Check for duplicated price in a single market
	// Can only be a one price per pair per contract
	type MarketPrice struct {
		priceDenom string
		assetDenom string
		price      uint64
	}

	// Check for duplication in longbook
	longBookPriceMap := make(map[MarketPrice]struct{})
	for _, elem := range cs.LongBookList {
		priceElem := MarketPrice{
			priceDenom: elem.Entry.PriceDenom,
			assetDenom: elem.Entry.AssetDenom,
			price:      elem.Price.BigInt().Uint64(),
		}
		if _, ok := longBookPriceMap[priceElem]; ok {
			return fmt.Errorf("duplicated price for longBook")
		}
		longBookPriceMap[priceElem] = struct{}{}
	}
	// Check for duplication in shortbook
	shortBookPriceMap := make(map[MarketPrice]struct{})
	for _, elem := range cs.ShortBookList {
		priceElem := MarketPrice{
			priceDenom: elem.Entry.PriceDenom,
			assetDenom: elem.Entry.AssetDenom,
			price:      elem.Price.BigInt().Uint64(),
		}
		if _, ok := shortBookPriceMap[priceElem]; ok {
			return fmt.Errorf("duplicated price for shortBook")
		}
		shortBookPriceMap[priceElem] = struct{}{}
	}
	return nil
}
