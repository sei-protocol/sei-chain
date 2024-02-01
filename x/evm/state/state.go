package state

import (
	"bytes"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *DBImpl) CreateAccount(acc common.Address) {
	// clear any existing state but keep balance untouched
	s.clearAccountState(acc)
	s.MarkAccount(acc, AccountCreated)
}

func (s *DBImpl) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(s.snapshottedCtxs[0], addr, hash)
}

func (s *DBImpl) GetState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(s.ctx, addr, hash)
}

func (s *DBImpl) getState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	return s.k.GetState(ctx, addr, hash)
}

func (s *DBImpl) SetState(addr common.Address, key common.Hash, val common.Hash) {
	s.k.SetState(s.ctx, addr, key, val)
}

func (s *DBImpl) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	val := s.k.PrefixStore(s.ctx, types.TransientStateKeyForAddress(s.ctx, addr)).Get(key[:])
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (s *DBImpl) SetTransientState(addr common.Address, key, val common.Hash) {
	s.k.PrefixStore(s.ctx, types.TransientStateKeyForAddress(s.ctx, addr)).Set(key[:], val[:])
}

// burns account's balance
// clear account's state except the transient state (in Ethereum transient states are
// still available even after self destruction in the same tx)
func (s *DBImpl) SelfDestruct(acc common.Address) {
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, acc); ok {
		// remove the association
		s.k.DeleteAddressMapping(s.ctx, seiAddr, acc)
	}

	s.SubBalance(acc, s.GetBalance(acc))

	// clear account state
	s.clearAccountState(acc)

	// mark account as self-destructed
	s.MarkAccount(acc, AccountDeleted)
}

func (s *DBImpl) Selfdestruct6780(acc common.Address) {
	// only self-destruct if acc is newly created in the same block
	if s.Created(acc) {
		s.SelfDestruct(acc)
	}
}

// the Ethereum semantics of HasSelfDestructed checks if the account is self destructed in the
// **CURRENT** block
func (s *DBImpl) HasSelfDestructed(acc common.Address) bool {
	store := s.k.PrefixStore(s.ctx, types.AccountTransientStateKey(s.ctx))
	return bytes.Equal(store.Get(acc[:]), AccountDeleted)
}

func (s *DBImpl) Snapshot() int {
	newCtx := s.ctx.WithMultiStore(s.ctx.MultiStore().CacheMultiStore())
	s.snapshottedCtxs = append(s.snapshottedCtxs, s.ctx)
	s.ctx = newCtx
	s.snapshottedLogs = append(s.snapshottedLogs, s.logs)
	s.logs = []*ethtypes.Log{}
	return len(s.snapshottedCtxs) - 1
}

func (s *DBImpl) RevertToSnapshot(rev int) {
	s.ctx = s.snapshottedCtxs[rev]
	s.snapshottedCtxs = s.snapshottedCtxs[:rev]
	s.logs = s.snapshottedLogs[rev]
	s.snapshottedLogs = s.snapshottedLogs[:rev]
	s.Snapshot()
}

func (s *DBImpl) clearAccountState(acc common.Address) {
	s.k.PurgePrefix(s.ctx, types.StateKey(acc))
	deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeKeyPrefix), acc[:])
	deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeSizeKeyPrefix), acc[:])
	deleteIfExists(s.k.PrefixStore(s.ctx, types.CodeHashKeyPrefix), acc[:])
	deleteIfExists(s.k.PrefixStore(s.ctx, types.NonceKeyPrefix), acc[:])
}

func (s *DBImpl) MarkAccount(acc common.Address, status []byte) {
	store := s.k.PrefixStore(s.ctx, types.AccountTransientStateKey(s.ctx))
	if status == nil {
		store.Delete(acc[:])
	} else {
		store.Set(acc[:], status)
	}
}

func (s *DBImpl) Created(acc common.Address) bool {
	store := s.k.PrefixStore(s.ctx, types.AccountTransientStateKey(s.ctx))
	return bytes.Equal(store.Get(acc[:]), AccountCreated)
}

func (s *DBImpl) SetStorage(addr common.Address, states map[common.Hash]common.Hash) {
	s.clearAccountState(addr)
	for key, val := range states {
		s.SetState(addr, key, val)
	}
}

func deleteIfExists(store storetypes.KVStore, key []byte) {
	if store.Has(key) {
		store.Delete(key)
	}
}
