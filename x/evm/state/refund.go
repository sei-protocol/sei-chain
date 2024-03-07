package state

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/utils/logging"
)

func (s *DBImpl) AddRefund(gas uint64) {
	logging.Info(s.ctx, fmt.Sprintf("Adding refund %d", gas), LoggingFeature)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, s.GetRefund()+gas)
	s.tempStateCurrent.transientModuleStates[string(GasRefundKey)] = bz
}

// Copied from go-ethereum as-is
// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *DBImpl) SubRefund(gas uint64) {
	logging.Info(s.ctx, fmt.Sprintf("Subtracting refund %d", gas), LoggingFeature)
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
