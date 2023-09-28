package state

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Exist reports whether the given account exists in state.
// Notably this should also return true for self-destructed accounts.
func (s *StateDBImpl) Exist(addr common.Address) bool {
	// if there is any entry under addr, it exists
	store := s.prefixStore(addr)
	iter := store.Iterator(nil, nil)
	if iter.Valid() {
		return true
	}

	// if there is code under addr, it exists
	if s.GetCodeHash(addr).Cmp(common.Hash{}) != 0 {
		return true
	}

	// go-ethereum impl considers just-deleted accounts as "exist" as well
	if _, ok := s.selfDestructedAccs[addr.String()]; ok {
		return true
	}

	return false
}

// Empty returns whether the given account is empty. Empty
// is defined according to EIP161 (balance = nonce = code = 0).
func (s *StateDBImpl) Empty(addr common.Address) bool {
	return s.GetBalance(addr).Cmp(big.NewInt(0)) == 0 && s.GetNonce(addr) == 0 && s.GetCodeHash(addr).Cmp(common.Hash{}) == 0
}
