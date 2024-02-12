package state

import (
	"encoding/binary"
	"fmt"
)

func (s *DBImpl) AddRefund(gas uint64) {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, s.GetRefund()+gas)
	s.tempStateCurrent.transientModuleStates[string(GasRefundKey)] = bz
}

// Copied from go-ethereum as-is
// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *DBImpl) SubRefund(gas uint64) {
	refund := s.GetRefund()
	if gas > refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, refund))
	}
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, refund-gas)
	s.tempStateCurrent.transientModuleStates[string(GasRefundKey)] = bz
}

func (s *DBImpl) GetRefund() uint64 {
	bz, found := s.getTransientModule(GasRefundKey)
	if !found || bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}
