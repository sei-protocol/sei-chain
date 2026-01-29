package state

import (
	"encoding/binary"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

type journalEntry interface {
	// revert undoes the changes introduced by this journal entry.
	revert(*DBImpl)
}

type (
	accountStatusChange struct {
		account common.Address
		prev    []byte
	}

	addLogChange struct{}

	refundChange struct {
		prev uint64
	}

	// Changes to the access list
	accessListAddAccountChange struct {
		address common.Address
	}
	accessListAddSlotChange struct {
		address common.Address
		slot    common.Hash
	}

	// Changes to transient storage
	transientStorageChange struct {
		account       common.Address
		key, prevalue common.Hash
	}

	surplusChange struct {
		delta sdk.Int
	}

	watermark struct {
		version int
	}
)

func (e *accessListAddAccountChange) revert(s *DBImpl) {
	delete(s.tempState.transientAccessLists.Addresses, e.address)
}

func (e *accessListAddSlotChange) revert(s *DBImpl) {
	// since slot change always comes after address change, and revert
	// happens in reverse order, the address access list hasn't been
	// cleared at this point.
	idx := s.tempState.transientAccessLists.Addresses[e.address]
	slotsList := s.tempState.transientAccessLists.Slots
	slots := slotsList[idx]
	delete(slots, e.slot)
	if len(slots) == 0 {
		s.tempState.transientAccessLists.Slots = append(slotsList[:idx], slotsList[idx+1:]...)
		s.tempState.transientAccessLists.Addresses[e.address] = -1
	}
}

func (e *surplusChange) revert(s *DBImpl) {
	s.tempState.surplus = s.tempState.surplus.Sub(e.delta)
}

func (e *addLogChange) revert(s *DBImpl) {
	s.tempState.logs = s.tempState.logs[:len(s.tempState.logs)-1]
}

func (e *refundChange) revert(s *DBImpl) {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, e.prev)
	s.tempState.transientModuleStates[string(GasRefundKey)] = bz
}

func (e *transientStorageChange) revert(s *DBImpl) {
	states := s.tempState.transientStates[e.account.Hex()]
	if e.prevalue.Cmp(common.Hash{}) == 0 {
		delete(states, e.key.Hex())
		if len(states) == 0 {
			delete(s.tempState.transientStates, e.account.Hex())
		}
	} else {
		states[e.key.Hex()] = e.prevalue
	}
}

func (e *watermark) revert(s *DBImpl) {}

func (e *accountStatusChange) revert(s *DBImpl) {
	accts := s.tempState.transientAccounts
	if e.prev == nil {
		delete(accts, e.account.Hex())
	} else {
		accts[e.account.Hex()] = e.prev
	}
}
