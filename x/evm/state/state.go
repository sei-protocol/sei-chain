package state

import (
	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func (s *StateDBImpl) CreateAccount(acc common.Address) {
	// clear any existing state but keep balance untouched
	s.clearAccountState(acc)

	s.created[acc.String()] = struct{}{}
	delete(s.selfDestructedAccs, acc.String())
}

func (s *StateDBImpl) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(addr, hash, func(kv sdk.KVStore, h common.Hash) []byte { return kv.GetCommitted(h[:]) })
}

func (s *StateDBImpl) GetState(addr common.Address, hash common.Hash) common.Hash {
	return s.getState(addr, hash, func(kv sdk.KVStore, h common.Hash) []byte { return kv.Get(h[:]) })
}

func (s *StateDBImpl) getState(addr common.Address, hash common.Hash, getter func(sdk.KVStore, common.Hash) []byte) common.Hash {
	val := getter(s.prefixStore(addr), hash)
	if val == nil {
		return common.Hash{}
	}
	return common.BytesToHash(val)
}

func (s *StateDBImpl) SetState(addr common.Address, key common.Hash, val common.Hash) {
	s.prefixStore(addr).Set(key[:], val[:])
}

func (s *StateDBImpl) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	if addrState, ok := s.transientStorage[addr.String()]; !ok {
		return common.Hash{}
	} else if val, ok := addrState[key.String()]; !ok {
		return common.Hash{}
	} else {
		return common.BytesToHash(val)
	}
}

func (s *StateDBImpl) SetTransientState(addr common.Address, key, value common.Hash) {
	addrKey := addr.String()
	if addrState, ok := s.transientStorage[addrKey]; !ok {
		s.transientStorage[addrKey] = map[string][]byte{key.String(): value[:]}
	} else {
		addrState[key.String()] = value[:]
	}
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
	s.selfDestructedAccs[acc.String()] = struct{}{}
	delete(s.created, acc.String())
}

func (s *StateDBImpl) SelfDestruct6780(acc common.Address) {
	// only self-destruct if acc is newly created in the same block
	if _, ok := s.created[acc.String()]; ok {
		s.SelfDestruct(acc)
	}
}

// the Ethereum semantics of HasSelfDestructed checks if the account is self destructed in the
// **CURRENT** block
func (s *StateDBImpl) HasSelfDestructed(acc common.Address) bool {
	_, ok := s.selfDestructedAccs[acc.String()]
	return ok
}

func (s *StateDBImpl) prefixStore(addr common.Address) sdk.KVStore {
	store := s.ctx.KVStore(s.k.GetStoreKey())
	pref := types.StateKey(addr)
	return prefix.NewStore(store, pref)
}

func (s *StateDBImpl) clearAccountState(acc common.Address) {
	store := s.prefixStore(acc)
	iter := store.Iterator(nil, nil)
	keys := [][]byte{}
	for ; iter.Valid(); iter.Next() {
		keys = append(keys, iter.Key())
	}
	iter.Close()
	for _, key := range keys {
		store.Delete(key)
	}
}
