package state

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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
	idx, ok := s.tempState.transientAccessLists.Addresses[e.address]
	// If the address was already removed or has no slots (idx == -1),
	// there is nothing to revert.
	if !ok || idx == -1 {
		return
	}
	slotsList := s.tempState.transientAccessLists.Slots
	// Bounds check in case a prior revert already modified the slots slice.
	if idx >= len(slotsList) {
		return
	}
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
	states := s.tempState.transientStates[e.account]
	if e.prevalue.Cmp(common.Hash{}) == 0 {
		// If the per-account transient map was already removed by a later revert,
		// there is nothing to delete.
		if states == nil {
			return
		}
		delete(states, e.key)
		if len(states) == 0 {
			delete(s.tempState.transientStates, e.account)
		}
	} else {
		// A prior revert may have deleted the per-account map when it became empty.
		// Re-create it so we can restore a non-zero prevalue.
		if states == nil {
			states = make(map[common.Hash]common.Hash, 4)
			s.tempState.transientStates[e.account] = states
		}
		states[e.key] = e.prevalue
	}
}

func (e *watermark) revert(s *DBImpl) {}

func (e *accountStatusChange) revert(s *DBImpl) {
	accts := s.tempState.transientAccounts
	if e.prev == nil {
		delete(accts, e.account)
	} else {
		accts[e.account] = e.prev
	}
}
