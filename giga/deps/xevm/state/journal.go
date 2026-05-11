package state

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type journalEntry interface {
	// revert undoes the changes introduced by this journal entry.
	revert(*DBImpl)
}

// revision marks a snapshot point in the journal.
type revision struct {
	id           int
	journalIndex int
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

	// storageChange records a KV storage mutation so it can be reverted.
	storageChange struct {
		addr common.Address
		key  common.Hash
		prev common.Hash
	}

	// codeChange records a code mutation so it can be reverted.
	codeChange struct {
		addr           common.Address
		prevCode       []byte
		prevCodeExists bool
		prevMapping    addressMappingState
	}

	// nonceChange records a nonce mutation so it can be reverted.
	nonceChange struct {
		addr       common.Address
		prev       uint64
		prevExists bool
	}

	// balanceChange records an Add or Sub balance so it can be reverted.
	balanceChange struct {
		evmAddr common.Address
		seiAddr sdk.AccAddress
		usei    sdk.Int
		wei     sdk.Int
		isAdd   bool // true if AddBalance was called
	}

	// createAccountChange records the previous state cleared by clearAccountStateJournaled.
	createAccountChange struct {
		addr            common.Address
		prevCode        []byte
		prevCodeExists  bool
		prevNonce       uint64
		prevNonceExists bool
		prevSlots       map[common.Hash]common.Hash
	}

	// deleteMappingChange records a DeleteAddressMapping so it can be reverted.
	deleteMappingChange struct {
		evmAddr common.Address
		seiAddr sdk.AccAddress
	}

	addressMappingState struct {
		exists        bool
		seiAddr       sdk.AccAddress
		accountExists bool
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

func (e *accountStatusChange) revert(s *DBImpl) {
	accts := s.tempState.transientAccounts
	if e.prev == nil {
		delete(accts, e.account.Hex())
	} else {
		accts[e.account.Hex()] = e.prev
	}
}

func (e *storageChange) revert(s *DBImpl) {
	s.k.SetState(s.ctx, e.addr, e.key, e.prev)
}

func (e *codeChange) revert(s *DBImpl) {
	restoreCode(s, e.addr, e.prevCode, e.prevCodeExists)
	e.prevMapping.restore(s, e.addr)
}

func (e *nonceChange) revert(s *DBImpl) {
	restoreNonce(s, e.addr, e.prev, e.prevExists)
}

func (e *balanceChange) revert(s *DBImpl) {
	// Suppress events on revert
	ctx := s.ctx.WithEventManager(sdk.NewEventManager())
	denom := s.k.GetBaseDenom(s.ctx)
	if e.isAdd {
		// Was AddBalance: reverse by subtracting
		if err := s.k.BankKeeper().SubUnlockedCoins(ctx, e.seiAddr, sdk.NewCoins(sdk.NewCoin(denom, e.usei)), true); err != nil {
			panic(fmt.Sprintf("balanceChange revert SubUnlockedCoins: %v", err))
		}
		if err := s.k.BankKeeper().SubWei(ctx, e.seiAddr, e.wei); err != nil {
			panic(fmt.Sprintf("balanceChange revert SubWei: %v", err))
		}
	} else {
		// Was SubBalance: reverse by adding
		if err := s.k.BankKeeper().AddCoins(ctx, e.seiAddr, sdk.NewCoins(sdk.NewCoin(denom, e.usei)), true); err != nil {
			panic(fmt.Sprintf("balanceChange revert AddCoins: %v", err))
		}
		if err := s.k.BankKeeper().AddWei(ctx, e.seiAddr, e.wei); err != nil {
			panic(fmt.Sprintf("balanceChange revert AddWei: %v", err))
		}
	}
}

func (e *createAccountChange) revert(s *DBImpl) {
	restoreCode(s, e.addr, e.prevCode, e.prevCodeExists)
	restoreNonce(s, e.addr, e.prevNonce, e.prevNonceExists)
	for k, v := range e.prevSlots {
		s.k.SetState(s.ctx, e.addr, k, v)
	}
}

func (e *deleteMappingChange) revert(s *DBImpl) {
	ctx := s.ctx.WithEventManager(sdk.NewEventManager())
	s.k.SetAddressMapping(ctx, e.seiAddr, e.evmAddr)
}

func captureAddressMapping(s *DBImpl, addr common.Address) addressMappingState {
	seiAddr, ok := s.k.GetSeiAddress(s.ctx, addr)
	if !ok {
		seiAddr = s.k.GetSeiAddressOrDefault(s.ctx, addr)
	}
	return addressMappingState{
		exists:        ok,
		seiAddr:       append(sdk.AccAddress(nil), seiAddr...),
		accountExists: s.k.AccountKeeper().HasAccount(s.ctx, seiAddr),
	}
}

func (m addressMappingState) restore(s *DBImpl, addr common.Address) {
	currentSeiAddr, ok := s.k.GetSeiAddress(s.ctx, addr)
	if ok && (!m.exists || !currentSeiAddr.Equals(m.seiAddr)) {
		s.k.DeleteAddressMapping(s.ctx, currentSeiAddr, addr)
	}
	if m.exists && (!ok || !currentSeiAddr.Equals(m.seiAddr)) {
		ctx := s.ctx.WithEventManager(sdk.NewEventManager())
		s.k.SetAddressMapping(ctx, m.seiAddr, addr)
	}
	if !m.accountExists {
		if acc := s.k.AccountKeeper().GetAccount(s.ctx, m.seiAddr); acc != nil {
			s.k.AccountKeeper().RemoveAccount(s.ctx, acc)
		}
	}
}

func restoreCode(s *DBImpl, addr common.Address, code []byte, exists bool) {
	if !exists {
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeKeyPrefix), addr[:])
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix), addr[:])
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix), addr[:])
		return
	}

	if code == nil {
		code = []byte{}
	}
	s.k.PrefixStore(s.ctx, types.CodeKeyPrefix).Set(addr[:], code)

	length := make([]byte, 8)
	binary.BigEndian.PutUint64(length, uint64(len(code)))
	s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix).Set(addr[:], length)

	hash := crypto.Keccak256Hash(code)
	s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix).Set(addr[:], hash[:])
}

func restoreNonce(s *DBImpl, addr common.Address, nonce uint64, exists bool) {
	if !exists {
		deleteIfExists(s.k.PrefixStore(s.ctx, types.NonceKeyPrefix), addr[:])
		return
	}
	s.k.SetNonce(s.ctx, addr, nonce)
}
