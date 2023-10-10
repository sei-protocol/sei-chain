package state

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) AddRefund(gas uint64) {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, s.GetRefund()+gas)
	store := s.k.PrefixStore(s.ctx, types.TransientModuleStateKeyPrefix)
	store.Set(GasRefundKey, bz)
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
	store := s.k.PrefixStore(s.ctx, types.TransientModuleStateKeyPrefix)
	store.Set(GasRefundKey, bz)
}

func (s *DBImpl) GetRefund() uint64 {
	store := s.k.PrefixStore(s.ctx, types.TransientModuleStateKeyPrefix)
	bz := store.Get(GasRefundKey)
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}
