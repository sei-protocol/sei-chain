package state

import (
	"github.com/ethereum/go-ethereum/common"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeHash(s.ctx, addr)
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCode(s.ctx, addr)
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	s.k.SetCode(s.ctx, addr, code)
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeSize(s.ctx, addr)
}
