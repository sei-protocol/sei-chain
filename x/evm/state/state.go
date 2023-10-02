package state

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	AccountCreated = []byte{0x01}
	AccountDeleted = []byte{0x02}
)

func (s *StateDBImpl) CreateAccount(acc common.Address) {
	// clear any existing state but keep balance untouched
	s.clearAccountState(acc)
	s.markAccount(acc, AccountCreated)
}

func (s *StateDBImpl) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(s.snapshottedCtxs[0], addr, hash)
}

func (s *StateDBImpl) GetState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(s.ctx, addr, hash)
}

func (s *StateDBImpl) getState(ctx sdk.Context, addr common.Address, hash common.Hash) common.Hash {
	val := s.k.PrefixStore(ctx, types.StateKey(addr)).Get(hash[:])
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (s *StateDBImpl) SetState(addr common.Address, key common.Hash, val common.Hash) {
	s.k.PrefixStore(s.ctx, types.StateKey(addr)).Set(key[:], val[:])
}

func (s *StateDBImpl) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	val := s.k.PrefixStore(s.ctx, types.TransientStateKey(addr)).Get(key[:])
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (s *StateDBImpl) SetTransientState(addr common.Address, key, val common.Hash) {
	s.k.PrefixStore(s.ctx, types.TransientStateKey(addr)).Set(key[:], val[:])
}

// burns account's balance
// clear account's state except the transient state (in Ethereum transient states are
// still available even after self destruction in the same tx)
func (s *StateDBImpl) SelfDestruct(acc common.Address) {
	var balance sdk.Coin
	if seiAddr, ok := s.k.GetSeiAddress(s.ctx, acc); ok {
		// send all useis from seiAddr to the EVM module
		balance = s.k.BankKeeper().GetBalance(s.ctx, seiAddr, s.k.GetBaseDenom(s.ctx))
		if balance.Amount.Int64() != 0 {
			if err := s.k.BankKeeper().SendCoinsFromAccountToModule(s.ctx, seiAddr, types.ModuleName, sdk.NewCoins(balance)); err != nil {
				s.err = err
				return
			}
		}
		// remove the association
		s.k.DeleteAddressMapping(s.ctx, seiAddr, acc)
	} else {
		// get old EVM balance
		balance = sdk.NewCoin(s.k.GetBaseDenom(s.ctx), sdk.NewIntFromUint64(s.k.GetBalance(s.ctx, acc)))
		// set EVM balance to 0
		s.k.SetOrDeleteBalance(s.ctx, acc, 0)
	}

	// burn all useis from the destructed account
	if balance.Amount.Int64() != 0 {
		if err := s.k.BankKeeper().BurnCoins(s.ctx, types.ModuleName, sdk.NewCoins(balance)); err != nil {
			s.err = err
			return
		}
	}

	// clear account state
	s.clearAccountState(acc)

	// mark account as self-destructed
	s.markAccount(acc, AccountDeleted)
}

func (s *StateDBImpl) SelfDestruct6780(acc common.Address) {
	// only self-destruct if acc is newly created in the same block
	if s.created(acc) {
		s.SelfDestruct(acc)
	}
}

// the Ethereum semantics of HasSelfDestructed checks if the account is self destructed in the
// **CURRENT** block
func (s *StateDBImpl) HasSelfDestructed(acc common.Address) bool {
	store := s.k.PrefixStore(s.ctx, types.AccountTransientStateKeyPrefix)
	return bytes.Equal(store.Get(acc[:]), AccountDeleted)
}

func (s *StateDBImpl) Snapshot() int {
	newCtx := s.ctx.WithMultiStore(s.ctx.MultiStore().CacheMultiStore())
	s.snapshottedCtxs = append(s.snapshottedCtxs, s.ctx)
	s.ctx = newCtx
	return len(s.snapshottedCtxs) - 1
}

func (s *StateDBImpl) RevertToSnapshot(rev int) {
	s.ctx = s.snapshottedCtxs[rev]
	s.snapshottedCtxs = s.snapshottedCtxs[:rev]
	s.Snapshot()
}

func (s *StateDBImpl) clearAccountState(acc common.Address) {
	s.k.PurgePrefix(s.ctx, types.StateKey(acc))
}

func (s *StateDBImpl) markAccount(acc common.Address, status []byte) {
	store := s.k.PrefixStore(s.ctx, types.AccountTransientStateKeyPrefix)
	if status == nil {
		store.Delete(acc[:])
	} else {
		store.Set(acc[:], status)
	}
}

func (s *StateDBImpl) created(acc common.Address) bool {
	store := s.k.PrefixStore(s.ctx, types.AccountTransientStateKeyPrefix)
	return bytes.Equal(store.Get(acc[:]), AccountCreated)
}
