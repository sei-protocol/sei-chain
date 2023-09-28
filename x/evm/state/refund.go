package state

import "fmt"

func (s *StateDBImpl) AddRefund(gas uint64) {
	s.gasRefund += gas
}

// Copied from go-ethereum as-is
// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *StateDBImpl) SubRefund(gas uint64) {
	if gas > s.gasRefund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.gasRefund))
	}
	s.gasRefund -= gas
}

func (s *StateDBImpl) GetRefund() uint64 {
	return s.gasRefund
}
