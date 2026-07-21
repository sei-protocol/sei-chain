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

	// Change to a masked account's overlay storage (state override)
	storageOverrideChange struct {
		account common.Address
		key     common.Hash
		prev    common.Hash
	}

	// Removal of a masked account's overlay (state override) when the account is
	// (re)created or cleared, so the overlay no longer shadows the now-empty
	// storage. prev holds the removed overlay for restoration on revert.
	storageOverrideRemove struct {
		account common.Address
		prev    *storageOverride
	}

	// Installation or replacement of a masked account's overlay (state override).
	// prev is nil if the account had no overlay; otherwise a deep copy of the
	// replaced overlay for restoration on revert.
	storageOverrideInstall struct {
		account common.Address
		prev    *storageOverride
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
	states := s.tempState.transientStates[e.account.Hex()]
	if e.prevalue.Cmp(common.Hash{}) == 0 {
		// If the per-account transient map was already removed by a later revert,
		// there is nothing to delete.
		if states == nil {
			return
		}
		delete(states, e.key.Hex())
		if len(states) == 0 {
			delete(s.tempState.transientStates, e.account.Hex())
		}
	} else {
		// A prior revert may have deleted the per-account map when it became empty.
		// Re-create it so we can restore a non-zero prevalue.
		if states == nil {
			states = make(map[string]common.Hash)
			s.tempState.transientStates[e.account.Hex()] = states
		}
		states[e.key.Hex()] = e.prevalue
	}
}

func (e *storageOverrideChange) revert(s *DBImpl) {
	if ov, ok := s.tempState.storageOverrides[e.account]; ok {
		ov.current[e.key.Hex()] = e.prev
	}
}

func (e *storageOverrideRemove) revert(s *DBImpl) {
	if e.prev != nil {
		// Deep-copy so a shallow-copied journal entry shared across StateDB
		// copies cannot re-install the same mutable overlay into two DBs.
		s.tempState.storageOverrides[e.account] = e.prev.deepCopy()
	}
}

func (e *storageOverrideInstall) revert(s *DBImpl) {
	if e.prev == nil {
		delete(s.tempState.storageOverrides, e.account)
	} else {
		s.tempState.storageOverrides[e.account] = e.prev.deepCopy()
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
