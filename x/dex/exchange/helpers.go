package exchange

import (
	"strings"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func RemoveAllocations(
	orderEntry *types.OrderEntry,
	creatorsToQuantities map[string]uint64,
) bool {
	modifiedAny := false

	newAllocations := []uint64{}
	newAllocationCreators := []string{}
	newQuantity := uint64(0)
	for i, allocationCreator := range orderEntry.AllocationCreator {
		if quantity, ok := creatorsToQuantities[allocationCreator]; !ok {
			newAllocationCreators = append(newAllocationCreators, allocationCreator)
			newAllocations = append(newAllocations, orderEntry.Allocation[i])
			newQuantity += orderEntry.Allocation[i]
		} else {
			var newAllocation uint64
			if quantity == 0 {
				newAllocation = 0 // 0 quantity in the cancel request indicates that the entirety of outstanding order should be cancelled
			} else if quantity <= orderEntry.Allocation[i] {
				newAllocation = orderEntry.Allocation[i] - quantity
			} else {
				newAllocation = 0
			}
			if newAllocation > 0 {
				newAllocationCreators = append(newAllocationCreators, allocationCreator)
				newAllocations = append(newAllocations, newAllocation)
				newQuantity += newAllocation
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

	newAllocations := []uint64{}
	newAllocationCreators := []string{}
	newQuantity := uint64(0)
	for i, allocationCreator := range orderEntry.AllocationCreator {
		rawCreator := strings.Split(allocationCreator, types.FORMATTED_ACCOUNT_DELIMITER)[0]
		if creators.Contains(rawCreator) {
			modifiedAny = true
			continue
		}
		newAllocationCreators = append(newAllocationCreators, allocationCreator)
		newAllocations = append(newAllocations, orderEntry.Allocation[i])
		newQuantity += orderEntry.Allocation[i]
	}
	orderEntry.Allocation = newAllocations
	orderEntry.AllocationCreator = newAllocationCreators
	orderEntry.Quantity = newQuantity
	return modifiedAny
}
