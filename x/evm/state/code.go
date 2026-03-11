package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func (s *DBImpl) GetCodeHash(addr common.Address) common.Hash {
	profile, start := s.startGetterProfile("db_get_code_hash")
	defer finishGetterProfile(profile, start, "db_get_code_hash")
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.codeHash[addr]; ok {
			if profile != nil {
				profile.AddCount("db_get_code_hash_cache_hit_count", 1)
			}
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
	profile, start := s.startGetterProfile("db_get_code")
	defer finishGetterProfile(profile, start, "db_get_code")
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.code[addr]; ok {
			if profile != nil {
				profile.AddCount("db_get_code_cache_hit_count", 1)
			}
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

		s.logger.OnCodeChange(addr, oldHash, oldCode, common.Hash(crypto.Keccak256(code)), code)
	}

	s.k.SetCode(s.ctx, addr, code)
	if s.cacheEnabled() {
		s.readCache.code[addr] = cloneBytes(code)
		s.readCache.codeHash[addr] = common.Hash(crypto.Keccak256Hash(code))
		s.readCache.codeSize[addr] = len(code)
	}
	return oldCode
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	profile, start := s.startGetterProfile("db_get_code_size")
	defer finishGetterProfile(profile, start, "db_get_code_size")
	s.k.PrepareReplayedAddr(s.ctx, addr)
	if s.cacheEnabled() {
		if cached, ok := s.readCache.codeSize[addr]; ok {
			if profile != nil {
				profile.AddCount("db_get_code_size_cache_hit_count", 1)
			}
			return cached
		}
	}
	codeSize := s.k.GetCodeSize(s.ctx, addr)
	if s.cacheEnabled() {
		s.readCache.codeSize[addr] = codeSize
	}
	return codeSize
}
