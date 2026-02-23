package state

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// all custom precompiles have an address greater than or equal to this address
var CustomPrecompileStartingAddr = common.HexToAddress("0x0000000000000000000000000000000000001001")

// Forked from go-ethereum, except journaling logic which is unnecessary with cacheKV

type accessList struct {
	Addresses map[common.Address]int
	Slots     []map[common.Hash]struct{}
}

func (s *DBImpl) AddressInAccessList(addr common.Address) bool {
	_, ok := s.getCurrentAccessList().Addresses[addr]
	return ok
}

func (s *DBImpl) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	al := s.getCurrentAccessList()
	idx, addrOk := al.Addresses[addr]
	if addrOk && idx != -1 {
		_, slotOk := al.Slots[idx][slot]
		return addrOk, slotOk
	}
	return addrOk, false
}

func (s *DBImpl) AddAddressToAccessList(addr common.Address) {
	al := s.getCurrentAccessList()
	if _, present := al.Addresses[addr]; present {
		return
	}
	al.Addresses[addr] = -1
	s.journal = append(s.journal, &accessListAddAccountChange{address: addr})
}

func (s *DBImpl) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	al := s.getCurrentAccessList()
	idx, addrPresent := al.Addresses[addr]
	if !addrPresent {
		s.AddAddressToAccessList(addr)
	}
	if !addrPresent || idx == -1 {
		// Address not present, or addr present but no slots there
		al.Addresses[addr] = len(al.Slots)
		slotmap := map[common.Hash]struct{}{slot: {}}
		al.Slots = append(al.Slots, slotmap)
		s.journal = append(s.journal, &accessListAddSlotChange{address: addr, slot: slot})
		return
	}
	slotmap := al.Slots[idx]
	if _, ok := slotmap[slot]; ok {
		// Slot already present, nothing to do (no journal entry needed)
		return
	}
	slotmap[slot] = struct{}{}
	s.journal = append(s.journal, &accessListAddSlotChange{address: addr, slot: slot})
}

func (s *DBImpl) Prepare(_ params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethtypes.AccessList) {
	s.AddAddressToAccessList(sender)
	if dest != nil {
		s.AddAddressToAccessList(*dest)
		// If it's a create-tx, the destination will be added inside evm.create
	}
	for _, addr := range precompiles {
		// skip any custom precompile
		if addr.Cmp(CustomPrecompileStartingAddr) >= 0 {
			if !s.ctx.IsTracing() {
				continue
			}
			if s.ctx.ChainID() != "pacific-1" || s.ctx.BlockHeight() >= 94496767 {
				continue
			}
		}
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

func (s *DBImpl) getCurrentAccessList() *accessList {
	return s.tempState.transientAccessLists
}
