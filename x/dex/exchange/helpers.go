package exchange

import (
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
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

func RemoveAllocations(
	orderEntry *types.OrderEntry,
	creatorsToQuantities map[string]sdk.Dec,
) bool {
	modifiedAny := false

	newAllocations := []sdk.Dec{}
	newAllocationCreators := []string{}
	newQuantity := sdk.ZeroDec()
	for i, allocationCreator := range orderEntry.AllocationCreator {
		if quantity, ok := creatorsToQuantities[allocationCreator]; !ok {
			newAllocationCreators = append(newAllocationCreators, allocationCreator)
			newAllocations = append(newAllocations, orderEntry.Allocation[i])
			newQuantity = newQuantity.Add(orderEntry.Allocation[i])
		} else {
			var newAllocation sdk.Dec
			if quantity.IsZero() {
				newAllocation = sdk.ZeroDec() // 0 quantity in the cancel request indicates that the entirety of outstanding order should be cancelled
			} else if quantity.LTE(orderEntry.Allocation[i]) {
				newAllocation = orderEntry.Allocation[i].Sub(quantity)
			} else {
				newAllocation = sdk.ZeroDec()
			}
			if newAllocation.IsPositive() {
				newAllocationCreators = append(newAllocationCreators, allocationCreator)
				newAllocations = append(newAllocations, newAllocation)
				newQuantity = newQuantity.Add(newAllocation)
			}
			modifiedAny = true
		}
	}
	orderEntry.Allocation = newAllocations
	orderEntry.AllocationCreator = newAllocationCreators
	orderEntry.Quantity = newQuantity
	return modifiedAny
}

func RemoveEntireAllocations(
	orderEntry *types.OrderEntry,
	creators utils.StringSet,
) bool {
	modifiedAny := false

	newAllocations := []sdk.Dec{}
	newAllocationCreators := []string{}
	newQuantity := sdk.ZeroDec()
	for i, allocationCreator := range orderEntry.AllocationCreator {
		rawCreator := strings.Split(allocationCreator, types.FORMATTED_ACCOUNT_DELIMITER)[0]
		if creators.Contains(rawCreator) {
			modifiedAny = true
			continue
		}
		newAllocationCreators = append(newAllocationCreators, allocationCreator)
		newAllocations = append(newAllocations, orderEntry.Allocation[i])
		newQuantity = newQuantity.Add(orderEntry.Allocation[i])
	}
	orderEntry.Allocation = newAllocations
	orderEntry.AllocationCreator = newAllocationCreators
	orderEntry.Quantity = newQuantity
	return modifiedAny
}
