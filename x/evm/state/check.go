package state

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Exist reports whether the given account exists in state.
// Notably this should also return true for self-destructed accounts.
func (s *DBImpl) Exist(addr common.Address) bool {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	// check if the address exists as a contract
	if s.GetCodeHash(addr).Cmp(common.Hash{}) != 0 {
		return true
	}

	// check if the address exists as an EOA
	if s.GetNonce(addr) > 0 {
		return true
	}

	// go-ethereum impl considers just-deleted accounts as "exist" as well
	return s.HasSelfDestructed(addr)
}

// Empty returns whether the given account is empty. Empty
// is defined according to EIP161 (balance = nonce = code = 0).
func (s *DBImpl) Empty(addr common.Address) bool {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.GetBalance(addr).Cmp(big.NewInt(0)) == 0 && s.GetNonce(addr) == 0 && s.GetCodeHash(addr).Cmp(common.Hash{}) == 0
}
