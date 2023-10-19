package state

import (
	"github.com/ethereum/go-ethereum/common"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	return s.k.GetCodeHash(s.ctx, addr)
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	return s.k.GetCode(s.ctx, addr)
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) {
	s.k.SetCode(s.ctx, addr, code)
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	return s.k.GetCodeSize(s.ctx, addr)
}
