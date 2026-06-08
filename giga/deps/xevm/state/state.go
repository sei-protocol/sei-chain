package state

import (
	"bytes"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
)

func (s *DBImpl) CreateAccount(acc common.Address) {
	// clear any existing state but keep balance untouched, journaled for revert
	if !s.ctx.IsTracing() {
		// too slow on historical DB so not doing it for tracing for now.
		// could cause tracing to be incorrect in theory.
		s.clearAccountStateJournaled(acc)
	}
	s.MarkAccount(acc, AccountCreated)
}

func (s *DBImpl) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	return s.k.GetState(s.committedCtx, addr, hash)
}

func (s *DBImpl) GetState(addr common.Address, hash common.Hash) common.Hash {
	return s.k.GetState(s.ctx, addr, hash)
}

func (s *DBImpl) SetState(addr common.Address, key common.Hash, val common.Hash) common.Hash {
	old := s.GetState(addr, key)
	if old == val {
		return old
	}
	if s.logger != nil && s.logger.OnStorageChange != nil {
		s.logger.OnStorageChange(addr, key, old, val)
	}

	s.k.SetState(s.ctx, addr, key, val)
	s.journal = append(s.journal, &storageChange{addr: addr, key: key, prev: old})
	return old
}

func (s *DBImpl) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	val, found := s.getTransientState(addr, key)
	if !found {
		return common.Hash{}
	}
	return val
}

func (s *DBImpl) SetTransientState(addr common.Address, key, val common.Hash) {
	st, ok := s.tempState.transientStates[addr.Hex()]
	if !ok {
		st = make(map[string]common.Hash)
		s.tempState.transientStates[addr.Hex()] = st
	}
	prev, ok := st[key.Hex()]
	if !ok {
		prev = common.Hash{}
	}
	st[key.Hex()] = val
	s.journal = append(s.journal, &transientStorageChange{account: addr, key: key, prevalue: prev})
}

// debits account's balance. The corresponding credit happens here:
// https://github.com/sei-protocol/go-ethereum/blob/master/core/vm/instructions.go#L825
// clear account's state except the transient state (in Ethereum transient states are
// still available even after self destruction in the same tx)
func (s *DBImpl) SelfDestruct(acc common.Address) uint256.Int {
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, acc); ok {
		// remove the association
		s.k.DeleteAddressMapping(s.ctx, seiAddr, acc)
		s.journal = append(s.journal, &deleteMappingChange{evmAddr: acc, seiAddr: seiAddr})
	}
	b := s.GetBalance(acc)
	s.SubBalance(acc, b, tracing.BalanceDecreaseSelfdestruct)

	// mark account as self-destructed
	s.MarkAccount(acc, AccountDeleted)
	return *b
}

func (s *DBImpl) SelfDestruct6780(acc common.Address) (uint256.Int, bool) {
	// only self-destruct if acc is newly created in the same block
	if s.Created(acc) {
		return s.SelfDestruct(acc), true
	}
	return *uint256.NewInt(0), false
}

// the Ethereum semantics of HasSelfDestructed checks if the account is self destructed in the
// **CURRENT** block
func (s *DBImpl) HasSelfDestructed(acc common.Address) bool {
	val, found := s.getTransientAccount(acc)
	if !found || val == nil {
		return false
	}
	return bytes.Equal(val, AccountDeleted)
}

// AnySelfDestructed reports whether any account was self-destructed in this tx,
// letting callers fall back to v2 before Finalize iterates the store and panics.
func (s *DBImpl) AnySelfDestructed() bool {
	for _, status := range s.tempState.transientAccounts {
		if bytes.Equal(status, AccountDeleted) {
			return true
		}
	}
	return false
}

// Snapshot records the current journal length as a revision and pushes the current
// EventManager onto the stack, creating a fresh one for subsequent events.
func (s *DBImpl) Snapshot() int {
	id := s.nextRevisionId
	s.nextRevisionId++
	s.validRevisions = append(s.validRevisions, revision{
		id:           id,
		journalIndex: len(s.journal),
	})
	// Push current EM and create a fresh one so reverted events are discarded.
	s.snapshottedEventManagers = append(s.snapshottedEventManagers, s.ctx.EventManager())
	s.ctx = s.ctx.WithEventManager(sdk.NewEventManager())
	return id
}

// RevertToSnapshot reverts all journal entries back to the snapshot identified by rev,
// restores the EventManager, and truncates the revision list.
func (s *DBImpl) RevertToSnapshot(rev int) {
	// Binary-search for the revision with the given id (like go-ethereum).
	idx := sort.Search(len(s.validRevisions), func(i int) bool {
		return s.validRevisions[i].id >= rev
	})
	if idx == len(s.validRevisions) || s.validRevisions[idx].id != rev {
		panic("invalid revision number")
	}
	snapshot := s.validRevisions[idx]

	// Revert journal entries in reverse order down to the snapshot point.
	for i := len(s.journal) - 1; i >= snapshot.journalIndex; i-- {
		s.journal[i].revert(s)
	}
	s.journal = s.journal[:snapshot.journalIndex]

	// Restore the EventManager that was active when the snapshot was taken.
	// snapshottedEventManagers has one entry per snapshot; idx corresponds to this snapshot.
	s.ctx = s.ctx.WithEventManager(s.snapshottedEventManagers[idx])
	s.snapshottedEventManagers = s.snapshottedEventManagers[:idx]

	// Truncate the revision list (removing this snapshot and any taken after it).
	s.validRevisions = s.validRevisions[:idx]
}

func (s *DBImpl) handleResidualFundsInDestructedAccounts(st *TemporaryState) {
	for a, status := range st.transientAccounts {
		if !bytes.Equal(status, AccountDeleted) {
			continue
		}
		acc := common.HexToAddress(a)
		residual := s.GetBalance(acc)
		if residual.ToBig().Cmp(utils.Big0) == 0 {
			continue
		}
		s.SubBalance(acc, residual, tracing.BalanceDecreaseSelfdestructBurn)
		// we don't want to really "burn" the token since it will mess up
		// total supply calculation, so we send them to fee collector instead
		s.AddBalance(s.coinbaseEvmAddress, residual, tracing.BalanceDecreaseSelfdestructBurn)
	}
}

func (s *DBImpl) clearAccountStateIfDestructed(st *TemporaryState) {
	for acc, status := range st.transientAccounts {
		if !bytes.Equal(status, AccountDeleted) {
			continue
		}
		s.clearAccountState(common.HexToAddress(acc))
	}
}

// clearAccountState unconditionally wipes code and storage for acc.
// Used by Finalize (self-destruct cleanup) and SetStorage. NOT journaled.
func (s *DBImpl) clearAccountState(acc common.Address) {
	if deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix), acc[:]) {
		s.k.PurgePrefix(s.ctx, types.StateKey(acc))
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeKeyPrefix), acc[:])
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix), acc[:])
		deleteIfExists(s.k.PrefixStore(s.ctx, types.NonceKeyPrefix), acc[:])
	}
}

// clearAccountStateJournaled wipes code, nonce, and storage for acc, recording
// the previous values in the journal so a RevertToSnapshot can restore them.
// Called from CreateAccount (when not tracing).
func (s *DBImpl) clearAccountStateJournaled(acc common.Address) {
	// Only clear if a code hash exists (mirrors clearAccountState logic).
	codeHashStore := s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix)
	if !codeHashStore.Has(acc[:]) {
		return
	}

	// Save previous state for potential revert.
	prevCode := s.k.GetCode(s.ctx, acc)
	prevNonce := s.k.GetNonce(s.ctx, acc)

	// Collect all storage slots for this account using GetAllKeyStrsInRange.
	// The prefix store's GetAllKeyStrsInRange returns raw parent-store keys,
	// so we strip the per-address state prefix to obtain each slot hash.
	prevSlots := make(map[common.Hash]common.Hash)
	statePrefix := types.StateKey(acc)
	stateStore := s.k.PrefixStore(s.ctx, statePrefix)
	rawKeys := stateStore.GetAllKeyStrsInRange(nil, nil)
	prefixLen := len(statePrefix)
	for _, raw := range rawKeys {
		if len(raw) <= prefixLen {
			continue
		}
		slotKey := common.BytesToHash([]byte(raw)[prefixLen:])
		slotVal := s.k.GetState(s.ctx, acc, slotKey)
		if slotVal != (common.Hash{}) {
			prevSlots[slotKey] = slotVal
		}
	}

	// Append journal entry before making changes.
	s.journal = append(s.journal, &createAccountChange{
		addr:      acc,
		prevCode:  prevCode,
		prevNonce: prevNonce,
		prevSlots: prevSlots,
	})

	// Clear the account state.
	s.clearAccountState(acc)
}

func (s *DBImpl) MarkAccount(acc common.Address, status []byte) {
	prev, ok := s.tempState.transientAccounts[acc.Hex()]
	if !ok {
		prev = nil
	}
	s.tempState.transientAccounts[acc.Hex()] = status
	s.journal = append(s.journal, &accountStatusChange{account: acc, prev: prev})
}

func (s *DBImpl) Created(acc common.Address) bool {
	val, found := s.getTransientAccount(acc)
	if !found || val == nil {
		return false
	}
	return bytes.Equal(val, AccountCreated)
}

func (s *DBImpl) SetStorage(addr common.Address, states map[common.Hash]common.Hash) {
	s.clearAccountState(addr)
	for key, val := range states {
		s.SetState(addr, key, val)
	}
}

func (s *DBImpl) getTransientAccount(acc common.Address) ([]byte, bool) {
	val, found := s.tempState.transientAccounts[acc.Hex()]
	return val, found
}

func (s *DBImpl) getTransientModule(key []byte) ([]byte, bool) {
	val, found := s.tempState.transientModuleStates[string(key)]
	return val, found
}

func (s *DBImpl) getTransientState(acc common.Address, key common.Hash) (common.Hash, bool) {
	var val common.Hash
	m, found := s.tempState.transientStates[acc.Hex()]
	if found {
		val, found = m[key.Hex()]
	}
	return val, found
}

func deleteIfExists(store storetypes.KVStore, key []byte) bool {
	if store.Has(key) {
		store.Delete(key)
		return true
	}
	return false
}
