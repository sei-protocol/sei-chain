package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeHash(s.ctx, addr)
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCode(s.ctx, addr)
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)

	oldCode := s.GetCode(addr)
	if s.logger != nil && s.logger.OnCodeChange != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		oldHash := s.GetCodeHash(addr)

		s.logger.OnCodeChange(addr, oldHash, oldCode, common.Hash(crypto.Keccak256(code)), code)
	}

	s.k.SetCode(s.ctx, addr, code)
	return oldCode
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeSize(s.ctx, addr)
}
