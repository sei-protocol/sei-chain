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
	s.k.PrepareReplayedAddr(s.ctx, addr)
	_, ok := s.getAccessList().Addresses[addr]
	return ok
}

func (s *DBImpl) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	al := s.getAccessList()
	idx, ok := al.Addresses[addr]
	if ok && idx != -1 {
		_, slotOk := al.Slots[idx][slot]
		return ok, slotOk
	}
	return ok, false
}

func (s *DBImpl) AddAddressToAccessList(addr common.Address) {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	al := s.getAccessList()
	defer s.saveAccessList(al)
	if _, present := al.Addresses[addr]; present {
		return
	}
	al.Addresses[addr] = -1
}

func (s *DBImpl) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	al := s.getAccessList()
	defer s.saveAccessList(al)
	idx, addrPresent := al.Addresses[addr]
	if !addrPresent || idx == -1 {
		// Address not present, or addr present but no slots there
		al.Addresses[addr] = len(al.Slots)
		slotmap := map[common.Hash]struct{}{slot: {}}
		al.Slots = append(al.Slots, slotmap)
		return
	}
	// There is already an (address,slot) mapping
	slotmap := al.Slots[idx]
	if _, ok := slotmap[slot]; !ok {
		slotmap[slot] = struct{}{}
	}
}

func (s *DBImpl) Prepare(_ params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethtypes.AccessList) {
	s.k.PrepareReplayedAddr(s.ctx, sender)
	s.k.PrepareReplayedAddr(s.ctx, coinbase)
	if dest != nil {
		s.k.PrepareReplayedAddr(s.ctx, *dest)
	}
	s.Snapshot()
	s.AddAddressToAccessList(sender)
	if dest != nil {
		s.AddAddressToAccessList(*dest)
		// If it's a create-tx, the destination will be added inside evm.create
	}
	for _, addr := range precompiles {
		s.AddAddressToAccessList(addr)
	}
	for _, el := range txAccesses {
		s.AddAddressToAccessList(el.Address)
		for _, key := range el.StorageKeys {
			s.AddSlotToAccessList(el.Address, key)
		}
	}
	s.AddAddressToAccessList(coinbase)
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
