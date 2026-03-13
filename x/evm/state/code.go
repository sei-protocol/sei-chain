package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.codeHash[addr]; ok {
			return cached
		}
	}
	codeHash := s.k.GetCodeHash(s.ctx, addr)
	if s.cacheEnabled() {
		s.readCache.codeHash[addr] = codeHash
	}
	return codeHash
}

func (s *DBImpl) GetCode(addr common.Address) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.code[addr]; ok {
			return cloneBytes(cached)
		}
	}
	code := s.k.GetCode(s.ctx, addr)
	if s.cacheEnabled() {
		s.readCache.code[addr] = cloneBytes(code)
	}
	return code
}

func (s *DBImpl) SetCode(addr common.Address, code []byte) []byte {
	s.k.PrepareReplayedAddr(s.ctx, addr)

	oldCode := s.GetCode(addr)
	if s.logger != nil && s.logger.OnCodeChange != nil {
		// The SetCode method could be modified to return the old code/hash directly.
		oldHash := s.GetCodeHash(addr)

		s.logger.OnCodeChange(addr, oldHash, oldCode, crypto.Keccak256Hash(code), code)
	}

	s.k.SetCode(s.ctx, addr, code)
	if s.cacheEnabled() {
		s.readCache.code[addr] = cloneBytes(code)
		s.readCache.codeHash[addr] = crypto.Keccak256Hash(code)
		s.readCache.codeSize[addr] = len(code)
	}
	return oldCode
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.codeSize[addr]; ok {
			return cached
		}
	}
	codeSize := s.k.GetCodeSize(s.ctx, addr)
	if s.cacheEnabled() {
		s.readCache.codeSize[addr] = codeSize
	}
	return codeSize
}
