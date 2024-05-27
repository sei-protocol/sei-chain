package state

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// Forked from go-ethereum, except journaling logic which is unnecessary with cacheKV

type accessList struct {
	Addresses map[common.Address]int     `json:"addresses"`
	Slots     []map[common.Hash]struct{} `json:"slots"`
}

func (s *DBImpl) AddressInAccessList(addr common.Address) bool {
	return true
}

func (s *DBImpl) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return true, true
}

func (s *DBImpl) AddAddressToAccessList(addr common.Address) {
}

func (s *DBImpl) AddSlotToAccessList(addr common.Address, slot common.Hash) {
}

func (s *DBImpl) Prepare(_ params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethtypes.AccessList) {
	s.k.PrepareReplayedAddr(s.ctx, sender)
	s.k.PrepareReplayedAddr(s.ctx, coinbase)
	if dest != nil {
		s.k.PrepareReplayedAddr(s.ctx, *dest)
	}
	s.Snapshot()
}

func (s *DBImpl) getAccessList() *accessList {
	bz, found := s.getTransientModule(AccessListKey)
	al := accessList{Addresses: make(map[common.Address]int)}
	if !found || bz == nil {
		return &al
	}
	if err := json.Unmarshal(bz, &al); err != nil {
		panic(err)
	}
	return &al
}

func (s *DBImpl) saveAccessList(al *accessList) {
	albz, err := json.Marshal(al)
	if err != nil {
		panic(err)
	}
	s.tempStateCurrent.transientModuleStates[string(AccessListKey)] = albz
}
