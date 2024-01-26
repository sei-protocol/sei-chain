package state

import (
	"github.com/ethereum/go-ethereum/common"
)

func (s *DBImpl) GetNonce(addr common.Address) uint64 {
	return s.k.GetNonce(s.ctx, addr)
}

func (s *DBImpl) SetNonce(addr common.Address, nonce uint64) {
	s.k.SetNonce(s.ctx, addr, nonce)
}
