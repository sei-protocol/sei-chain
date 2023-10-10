package state

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// Exist reports whether the given account exists in state.
// Notably this should also return true for self-destructed accounts.
func (s *DBImpl) Exist(addr common.Address) bool {
	// if there is any entry under addr, it exists
	store := s.k.PrefixStore(s.ctx, types.StateKey(addr))
	iter := store.Iterator(nil, nil)
	if iter.Valid() {
		return true
	}

	// if there is code under addr, it exists
	if s.GetCodeHash(addr).Cmp(common.Hash{}) != 0 {
		return true
	}

	// go-ethereum impl considers just-deleted accounts as "exist" as well
	return s.HasSelfDestructed(addr)
}

// Empty returns whether the given account is empty. Empty
// is defined according to EIP161 (balance = nonce = code = 0).
func (s *DBImpl) Empty(addr common.Address) bool {
	return s.GetBalance(addr).Cmp(big.NewInt(0)) == 0 && s.GetNonce(addr) == 0 && s.GetCodeHash(addr).Cmp(common.Hash{}) == 0
}
