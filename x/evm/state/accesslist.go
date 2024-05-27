package state

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// Always returns true for access list check

func (s *DBImpl) AddressInAccessList(addr common.Address) bool {
	return true
}

func (s *DBImpl) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return true, true
}

func (s *DBImpl) AddAddressToAccessList(addr common.Address) {}

func (s *DBImpl) AddSlotToAccessList(addr common.Address, slot common.Hash) {}

func (s *DBImpl) Prepare(_ params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethtypes.AccessList) {
	s.k.PrepareReplayedAddr(s.ctx, sender)
	s.k.PrepareReplayedAddr(s.ctx, coinbase)
	if dest != nil {
		s.k.PrepareReplayedAddr(s.ctx, *dest)
	}
	s.Snapshot()
}
