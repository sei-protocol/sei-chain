package state

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/holiman/uint256"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) CreateAccount(acc common.Address) {
	s.k.PrepareReplayedAddr(s.ctx, acc)
	// clear any existing state but keep balance untouched
	if !s.ctx.IsTracing() {
		// too slow on historical DB so not doing it for tracing for now.
		// could cause tracing to be incorrect in theory.
		s.clearAccountState(acc)
	}
	s.MarkAccount(acc, AccountCreated)
}

func (s *DBImpl) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(s.snapshottedCtxs[0], addr, hash)
}

func (s *DBImpl) GetState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(s.ctx, addr, hash)
}

func (s *DBImpl) getState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	s.k.PrepareReplayedAddr(ctx, addr)
	return s.k.GetState(ctx, addr, hash)
}

func (s *DBImpl) SetState(addr common.Address, key common.Hash, val common.Hash) common.Hash {
	s.k.PrepareReplayedAddr(s.ctx, addr)

	old := s.GetState(addr, key)
	if s.logger != nil && s.logger.OnStorageChange != nil {
		s.logger.OnStorageChange(addr, key, old, val)
	}

	s.k.SetState(s.ctx, addr, key, val)
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
	s.k.PrepareReplayedAddr(s.ctx, acc)
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, acc); ok {
		// remove the association
		s.k.DeleteAddressMapping(s.ctx, seiAddr, acc)
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

func (s *DBImpl) Snapshot() int {
	newCtx := s.ctx.WithMultiStore(s.ctx.MultiStore().CacheMultiStore()).WithEventManager(sdk.NewEventManager())
	s.snapshottedCtxs = append(s.snapshottedCtxs, s.ctx)
	s.ctx = newCtx
	version := len(s.snapshottedCtxs) - 1
	s.journal = append(s.journal, &watermark{version: version})
	return len(s.snapshottedCtxs) - 1
}

func (s *DBImpl) RevertToSnapshot(rev int) {
	// Add bounds checking
	if rev < 0 || rev >= len(s.snapshottedCtxs) {
		panic("invalid revision number")
	}

	s.ctx = s.snapshottedCtxs[rev]
	s.snapshottedCtxs = s.snapshottedCtxs[:rev]

	// Find the watermark index to truncate the journal
	watermarkIndex := -1
	for i := len(s.journal) - 1; i >= 0; i-- {
		entry := s.journal[i]
		entry.revert(s)
		if wm, ok := entry.(*watermark); ok && wm.version == rev {
			watermarkIndex = i
			break
		}
	}

	// Truncate the journal to remove reverted entries
	if watermarkIndex >= 0 {
		s.journal = s.journal[:watermarkIndex]
	}
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

func (s *DBImpl) clearAccountState(acc common.Address) {
	s.k.PrepareReplayedAddr(s.ctx, acc)
	if deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix), acc[:]) {
		s.k.PurgePrefix(s.ctx, types.StateKey(acc))
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeKeyPrefix), acc[:])
		deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix), acc[:])
		deleteIfExists(s.k.PrefixStore(s.ctx, types.NonceKeyPrefix), acc[:])
	}
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
