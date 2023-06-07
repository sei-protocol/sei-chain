package types

import (
	"sort"
	"sync"
)

type DeferredBankOperationMapping struct {
	deferredOperations map[string]Coins
	mappingLock        *sync.Mutex
}

func NewDeferredBankOperationMap() *DeferredBankOperationMapping {
	return &DeferredBankOperationMapping{
		deferredOperations: make(map[string]Coins),
		mappingLock:        &sync.Mutex{},
	}
}

// Get returns the current deferred balances for the passed in module account as well as a `found` bool.
// This is threadsafe since it acquires a lock on the mutex.
func (m *DeferredBankOperationMapping) Get(moduleAccount string) (Coins, bool) {
	m.mappingLock.Lock()
	defer m.mappingLock.Unlock()

	deferredAmount, ok := m.deferredOperations[moduleAccount]
	return deferredAmount, ok
}

// Get returns the current deferred balances for the passed in module account as well as a `found` bool.
// This is threadsafe since it acquires a lock on the mutex.
func (m *DeferredBankOperationMapping) Set(moduleAccount string, amount Coins) {
	m.mappingLock.Lock()
	defer m.mappingLock.Unlock()

	m.deferredOperations[moduleAccount] = amount
}

// SaturatingSub will subtract the given amount from the module account as long as the resulting balance is positive or zero.
// If there would be a remainder (eg. negative balance after subtraction), then it would subtract the full balance in the map
// and then return the remainder that was unable to be subtracted.
func (m *DeferredBankOperationMapping) SaturatingSub(moduleAccount string, amount Coins) Coins {
	m.mappingLock.Lock()
	defer m.mappingLock.Unlock()

	if deferredAmount, ok := m.deferredOperations[moduleAccount]; ok {
		newAmount, isNegative := deferredAmount.SafeSub(amount)
		if !isNegative {
			// this means that the subtraction FULLY succeeded, no remainders
			m.deferredOperations[moduleAccount] = newAmount
			// return empty remainder
			return Coins{}
		} else {
			// else there were some negative, we need to partition the results, and return the negative balances as positive instead as the remainder
			pos, neg := newAmount.PartitionSigned()
			// assign positives to map (anything that wasnt touched or had sufficient balance to process the subtraction fully)
			m.deferredOperations[moduleAccount] = pos
			// convert the negatives to positives to represent the remainder
			return neg.negative()
		}
	}
	// no entry, so we return the full amount as the remainder
	return amount
}

// If there's already a pending opposite operation then subtract it from that amount first
// returns true if amount was subtracted
func (m *DeferredBankOperationMapping) SafeSub(moduleAccount string, amount Coins) bool {
	m.mappingLock.Lock()
	defer m.mappingLock.Unlock()

	if deferredAmount, ok := m.deferredOperations[moduleAccount]; ok {
		newAmount, isNegative := deferredAmount.SafeSub(amount)
		if !isNegative {
			m.deferredOperations[moduleAccount] = newAmount
			return true
		}
	}
	return false
}

func (m *DeferredBankOperationMapping) UpsertMapping(moduleAccount string, amount Coins) {
	m.mappingLock.Lock()
	defer m.mappingLock.Unlock()

	newAmount := amount
	if v, ok := m.deferredOperations[moduleAccount]; ok {
		newAmount = v.Add(amount...)
	}
	m.deferredOperations[moduleAccount] = newAmount
}

func (m *DeferredBankOperationMapping) GetSortedKeys() []string {

	// Need to sort keys for deterministic iterating
	keys := make([]string, 0, len(m.deferredOperations))
	for key := range m.deferredOperations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *DeferredBankOperationMapping) RangeAndRemove(apply func(recipient string, amount Coins)) {
	m.mappingLock.Lock()
	defer m.mappingLock.Unlock()

	keys := m.GetSortedKeys()

	for _, moduleAccount := range keys {
		apply(moduleAccount, m.deferredOperations[moduleAccount])
	}

	for _, moduleAccount := range keys {
		delete(m.deferredOperations, moduleAccount)
	}
}
