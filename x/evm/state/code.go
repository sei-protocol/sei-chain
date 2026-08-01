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
	// nil codeCache means caching is disabled (simulation/RPC/trace); never allocate here.
	if s.codeCache != nil {
		if code, ok := s.codeCache[addr]; ok {
			return code
		}
	}
	code := s.k.GetCode(s.ctx, addr)
	if s.codeCache == nil {
		return code
	}
	// Cache a copy so callers cannot mutate the keeper/store-backed slice into
	// the tx memo, and keep empty code as nil to match Keeper.GetCode.
	if len(code) == 0 {
		s.codeCache[addr] = nil
		return nil
	}
	cached := make([]byte, len(code))
	copy(cached, code)
	s.codeCache[addr] = cached
	return cached
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
	if s.codeCache == nil {
		return oldCode
	}
	if len(code) == 0 {
		s.codeCache[addr] = nil
	} else {
		// Store a copy so later mutations of the caller's slice cannot corrupt the cache.
		cached := make([]byte, len(code))
		copy(cached, code)
		s.codeCache[addr] = cached
	}
	return oldCode
}

func (s *DBImpl) GetCodeSize(addr common.Address) int {
	s.k.PrepareReplayedAddr(s.ctx, addr)
	return s.k.GetCodeSize(s.ctx, addr)
}
