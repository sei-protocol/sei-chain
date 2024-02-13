package state

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

func (s *DBImpl) GetNonce(addr common.Address) uint64 {
	nonce := s.k.GetNonce(s.ctx, addr)
	fmt.Println("In GetNonce, addr = ", addr, " nonce = ", nonce)
	return nonce
}

func (s *DBImpl) SetNonce(addr common.Address, nonce uint64) {
	fmt.Println("In SetNonce, addr = ", addr, " nonce = ", nonce)
	s.k.SetNonce(s.ctx, addr, nonce)
}
